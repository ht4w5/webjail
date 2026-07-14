package tail

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	pollInterval = 250 * time.Millisecond
)

type fakeReader struct{}

func (*fakeReader) Read(p []byte) (n int, err error) {
	return 0, nil
}

type Config struct {
	// Seek to before tailing.
	Seek SeekInfo

	// Provide appended data as the file grows.
	Follow bool

	// Keep trying to open a file if it is inaccessible.
	// Ignored when Follow is false.
	Retry bool

	// Use PollWatcher for file changes.
	// FSNotifyWatcher is used if false.
	Poll bool

	// Disable seeking.
	// Enable this for named pipes (mkfifo).
	NoSeek bool

	// Line delimiter.
	Delimiter byte
}

func DefaultConfig() Config {
	return Config{
		Delimiter: '\n',
	}
}

// SeekInfo
type SeekInfo struct {
	Offset int64
	Whence int
}

type tailState int8

const (
	stateInit tailState = iota
	stateReady
	stateEOF
	stateNotOpen
	stateFailed
)

type Tail struct {
	cfg      Config
	filename string

	file    *os.File
	watcher Watcher
	reader  *bufio.Reader

	state   tailState
	failErr error

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func New(ctx context.Context, filename string, cfg Config) (*Tail, error) {
	if !cfg.Follow && cfg.Retry {
		return nil, errors.New("Retry set without Follow")
	}

	var watcher Watcher
	var err error
	if cfg.Poll {
		watcher, err = NewPollWatcher(filename, pollInterval)
		if err != nil {
			return nil, fmt.Errorf("failed to create PollWatcher: %w", err)
		}
	} else {
		watcher, err = NewFSNotifyWatcher(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create FSNotifyWatcher: %w", err)
		}
	}

	return newTailWithWatcher(ctx, filename, cfg, watcher), nil
}

func newTailWithWatcher(ctx context.Context, filename string, cfg Config, watcher Watcher) *Tail {
	ctx, cancel := context.WithCancel(ctx)

	return &Tail{
		cfg:      cfg,
		filename: filename,
		watcher:  watcher,
		reader:   bufio.NewReader(&fakeReader{}),
		state:    stateInit,
		ctx:      ctx,
		cancel:   cancel,
	}
}

var (
	ErrBufferTooShort = errors.New("buffer is too short to read complete line")
	ErrClosed         = errors.New("closed")
)

func (t *Tail) ReadLine(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for {
		switch t.state {
		case stateInit:
			// Try to open file
			file, err := os.Open(t.filename)
			if err != nil {
				t.failErr = err
				t.state = stateNotOpen
				continue
			} else {
				if !t.cfg.NoSeek {
					_, err = file.Seek(t.cfg.Seek.Offset, t.cfg.Seek.Whence)
					if err != nil {
						t.failErr = err
						t.state = stateFailed
						return 0, err
					}
				}
				t.file = file
				t.reader.Reset(file)
				t.state = stateReady
				continue
			}

		case stateReady:
			// Try to read line
			n := t.reader.Buffered()
			if n == 0 {
				if _, err := t.reader.Peek(1); err != nil {
					if errors.Is(err, io.EOF) {
						t.state = stateEOF
						continue
					} else {
						t.failErr = err
						t.state = stateFailed
						return 0, err
					}
				}
				n = t.reader.Buffered()
			}

			peeked, err := t.reader.Peek(n)
			if err != nil && !errors.Is(err, io.EOF) {
				t.failErr = err
				t.state = stateFailed
				return 0, err
			}

			idx := bytes.IndexByte(peeked, t.cfg.Delimiter)
			if idx < 0 {
				// Not a full line
				t.state = stateEOF
				continue
			}

			if len(b) < idx {
				return 0, ErrBufferTooShort
			}

			copied := copy(b, peeked[:idx])
			t.reader.Discard(idx + 1)
			return copied, nil

		case stateEOF:
			if !t.cfg.Follow {
				t.failErr = io.EOF
				t.state = stateFailed
				return 0, io.EOF
			}

			ch, err := t.watcher.BlockUntilChange(t.ctx)
			if err != nil {
				t.failErr = err
				t.state = stateFailed
				return 0, err
			}

			switch ch {
			case ChangeModified:
				t.state = stateReady
				continue
			case ChangeDeleted:
				err = t.file.Close()
				if err != nil {
					t.failErr = err
					t.state = stateFailed
					return 0, err
				}
				t.state = stateNotOpen
				continue
			case ChangeCreated:
				t.state = stateReady
				continue
			case ChangeTruncated:
				if !t.cfg.NoSeek {
					_, err := t.file.Seek(0, io.SeekStart)
					if err != nil {
						t.failErr = err
						t.state = stateFailed
						return 0, err
					}
				}
				t.reader.Reset(t.file)
				t.state = stateReady
				continue
			}

		case stateNotOpen:
			if !t.cfg.Retry {
				t.state = stateFailed
				return 0, errors.New("cannot open file")
			}

			ch, err := t.watcher.BlockUntilChange(t.ctx)
			if err != nil {
				t.failErr = err
				t.state = stateFailed
				return 0, err
			}

			switch ch {
			case ChangeCreated:
				// Try to open file
				file, err := os.Open(t.filename)
				if err != nil {
					continue
				} else {
					t.file = file
					t.reader.Reset(file)
					t.state = stateReady
					continue
				}

			case ChangeDeleted:
				t.failErr = errors.New("got ChangeDeleted in stateNotOpen")
				t.state = stateFailed
				return 0, t.failErr

			case ChangeModified:
				t.failErr = errors.New("got ChangeModified in stateNotOpen")
				t.state = stateFailed
				return 0, t.failErr

			case ChangeTruncated:
				t.failErr = errors.New("got ChangeModified in stateNotOpen")
				t.state = stateFailed
				return 0, t.failErr
			}

		case stateFailed:
			return 0, t.failErr
		}
	}
}

func (t *Tail) Close() error {
	t.cancel()
	t.mu.Lock()
	defer t.mu.Unlock()

	t.failErr = ErrClosed
	t.state = stateFailed
	if t.file != nil {
		if err := t.file.Close(); err != nil {
			return err
		}
	}
	return nil
}
