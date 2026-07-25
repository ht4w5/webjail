package ban

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/HT4w5/webjail/pkg/log"
	"github.com/rs/zerolog"
	"go.etcd.io/bbolt"
)

var bboltOptions = bbolt.Options{
	Timeout: 20 * time.Second,
}

var recordsBucket = []byte("records")

var bufPool = sync.Pool{
	New: func() any {
		return make([]byte, 0, 128)
	},
}

func putBuffer(b []byte) {
	bufPool.Put(b[:0])
}

func getBuffer() []byte {
	return bufPool.Get().([]byte)
}

var _ Jail = (*BBoltJail)(nil)

type BBoltJail struct {
	db        *bbolt.DB
	logger    zerolog.Logger
	retention time.Duration

	cancel context.CancelFunc
	done   chan struct{}
}

// NewBBoltJail creates a BBoltJail.
//
// retention specifies how long to keep records after their ExpiresAt.
func NewBBoltJail(path string, retention time.Duration, logger zerolog.Logger) (*BBoltJail, error) {
	db, err := bbolt.Open(path, 0600, &bboltOptions)
	if err != nil {
		return nil, fmt.Errorf("NewBBoltJail: open db failed: %w", err)
	}

	if retention < 0 {
		return nil, errors.New("NewBBoltJail: retention must not be negative")
	}

	j := &BBoltJail{
		db:        db,
		retention: retention,
		logger:    logger.With().Str(log.KeyModule, "ban.BBoltJail").Logger(),
	}

	return j, nil
}

func (j *BBoltJail) Start(ctx context.Context) error {
	err := j.db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(recordsBucket)
		return err
	})
	if err != nil {
		return fmt.Errorf("BBoltJail.Start: %w", err)
	}

	internalCtx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	j.done = make(chan struct{})

	go j.cleanupLoop(internalCtx)

	return nil
}

func (j *BBoltJail) Shutdown(ctx context.Context) (err error) {
	cancel := j.cancel
	done := j.done

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	err = errors.Join(err, j.db.Close())
	return err
}

func (j *BBoltJail) Add(ctx context.Context, r *Record) error {
	var kbuf [16]byte
	key := appendKey(kbuf[:0], r.Addr)
	buf := getBuffer()
	val := encodeRecord(buf, r)
	err := j.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(recordsBucket)
		return b.Put(key, val)
	})
	putBuffer(buf)
	return err
}

func (j *BBoltJail) Del(ctx context.Context, addr netip.Addr) error {
	var kbuf [16]byte
	key := appendKey(kbuf[:0], addr)
	return j.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(recordsBucket)
		return b.Delete(key)
	})
}

func (j *BBoltJail) Get(ctx context.Context, addr netip.Addr) (Record, error) {
	var kbuf [16]byte
	key := appendKey(kbuf[:0], addr)
	var rec Record
	err := j.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(recordsBucket)
		val := b.Get(key)
		if val == nil {
			return ErrNotFound
		}
		var decodeErr error
		rec, decodeErr = decodeRecord(addr, val)
		if decodeErr != nil {
			j.logger.Error().Err(decodeErr).Msg("failed to decode record")
			return decodeErr
		}
		return nil
	})
	return rec, err
}

// ForEach iterates over records in the [start, end] address range (inclusive).
// Iteration stops if f returns an error or ctx is cancelled.
func (j *BBoltJail) ForEach(ctx context.Context, start netip.Addr, end netip.Addr, f func(r *Record) error) error {
	var skbuf [16]byte
	var ekbuf [16]byte
	startKey := appendKey(skbuf[:0], start)
	endKey := appendKey(ekbuf[:0], end)

	return j.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(recordsBucket)
		c := b.Cursor()

		for k, v := c.Seek(startKey); k != nil && bytes.Compare(k, endKey) <= 0; k, v = c.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			addr, err := addrFromKey(k)
			if err != nil {
				return fmt.Errorf("BBoltJail.ForEach: %w", err)
			}
			rec, err := decodeRecord(addr, v)
			if err != nil {
				return fmt.Errorf("BBoltJail.ForEach: %w", err)
			}
			if err := f(&rec); err != nil {
				return err
			}
		}
		return nil
	})
}

func (j *BBoltJail) Stats() any {
	return j.db.Stats()
}

func (j *BBoltJail) cleanupLoop(ctx context.Context) {
	defer close(j.done)

	tickInterval := j.retention / 2
	if tickInterval < time.Second {
		tickInterval = time.Second
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.cleanupExpired()
		}
	}
}

func (j *BBoltJail) cleanupExpired() {
	now := time.Now()
	j.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(recordsBucket)
		c := b.Cursor()

		var toDelete [][]byte
		for k, v := c.First(); k != nil; k, v = c.Next() {
			expiresAt, err := getRecordExpiresAt(v)
			if err != nil {
				j.logger.Error().Stack().Err(err).Msg("failed decode record expires at")
				// delete corrupted
				kCopy := make([]byte, len(k))
				copy(kCopy, k)
				toDelete = append(toDelete, kCopy)
				continue
			}
			if now.After(expiresAt.Add(j.retention)) {
				kCopy := make([]byte, len(k))
				copy(kCopy, k)
				toDelete = append(toDelete, kCopy)
			}
		}

		for _, k := range toDelete {
			if err := b.Delete(k); err != nil {
				j.logger.Error().Stack().Err(err).Msg("delete record error")
			}
		}
		return nil
	})
}
