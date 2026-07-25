package filesize

import (
	"context"
	"encoding/binary"
	"errors"
	"time"
	"unsafe"

	"github.com/HT4w5/kv"
	"github.com/HT4w5/webjail/pkg/log"
	"github.com/rs/zerolog"
)

var _ Querier = (*Cache)(nil)

// Cache payload:
// K: url
// V: UnixNanoExpiresAt+Int64Size

type Cache struct {
	upstream Querier
	cache    *kv.Cache
	logger   zerolog.Logger

	ttl time.Duration
}

func NewCache(upstream Querier, ttl time.Duration, maxBytes int, logger zerolog.Logger) *Cache {
	return &Cache{
		upstream: upstream,
		cache:    kv.New(maxBytes),
		logger:   logger.With().Str(log.KeyModule, "filesize.Cache").Logger(),
		ttl:      ttl,
	}
}

func (c *Cache) Query(ctx context.Context, url string) (size int64, err error) {
	if url == "" {
		c.logger.Warn().Msg("query with empty url")
		return 0, ErrBadURL
	}

	var buf [16]byte
	k := unsafe.Slice(unsafe.StringData(url), len(url))
	val, ok := c.cache.HasGet(buf[:], k)
	if ok {
		if len(val) == 16 {
			t := time.Unix(0, int64(binary.LittleEndian.Uint64(val)))
			if t.After(time.Now()) {
				size = int64(binary.LittleEndian.Uint64(val[8:]))
				if size == NoInfo {
					err = ErrNoInfo
				}
				return
			}
		}
	}

	// Query upstream
	size, err = c.upstream.Query(ctx, url)
	if err != nil {
		if errors.Is(err, ErrNoInfo) {
			binary.LittleEndian.PutUint64(buf[:], uint64(time.Now().Add(c.ttl).UnixNano()))
			size = NoInfo
			binary.LittleEndian.PutUint64(buf[8:], uint64(size))
			c.cache.Set(k, buf[:])
		}
		return
	}

	binary.LittleEndian.PutUint64(buf[:], uint64(time.Now().Add(c.ttl).UnixNano()))
	binary.LittleEndian.PutUint64(buf[8:], uint64(size))
	c.cache.Set(k, buf[:])
	return
}
