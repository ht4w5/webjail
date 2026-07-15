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
	// Requires Follow to be set.
	Retry bool

	// Use PollWatcher for file changes.
	// FSNotifyWatcher is used if false.
	Poll bool

	// Disable seeking.
	// Enable this for named pipes (mkfifo).
	NoSeek bool

	// MaxLineSize sets the maximum line length in bytes.
	// Lines exceeding this are returned in chunks of MaxLineSize with error ErrMaxLineSizeExceeded.
	// Defaults to 4096 when <= 0.
	MaxLineSize int

	// Line delimiter.
	Delimiter byte
}

func DefaultConfig() Config {
	return Config{
		Delimiter:   '\n',
		MaxLineSize: 4096,
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

	if cfg.MaxLineSize <= 0 {
		cfg.MaxLineSize = 4096
	}

	return &Tail{
		cfg:      cfg,
		filename: filename,
		watcher:  watcher,
		reader:   bufio.NewReaderSize(&fakeReader{}, cfg.MaxLineSize),
		state:    stateInit,
		ctx:      ctx,
		cancel:   cancel,
	}
}

var (
	ErrMaxLineSizeExceeded = errors.New("line exceeds MaxLineSize")
	ErrShortBuffer         = io.ErrShortBuffer
	ErrClosed              = errors.New("closed")
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
				// No delimiter found
				if n >= t.cfg.MaxLineSize {
					// Line exceeds MaxLineSize
					// Return chunk
					n = t.cfg.MaxLineSize
					if len(b) < n {
						return 0, ErrShortBuffer
					}
					peeked, _ = t.reader.Peek(n)
					copied := copy(b, peeked[:n])
					t.reader.Discard(n)
					return copied, ErrMaxLineSizeExceeded
				}
				t.state = stateEOF
				continue
			}

			// Delimiter found
			// Chunk if line exceeds MaxLineSize
			if idx >= t.cfg.MaxLineSize {
				n = t.cfg.MaxLineSize
				if len(b) < n {
					return 0, ErrShortBuffer
				}
				peeked, _ = t.reader.Peek(n)
				copied := copy(b, peeked[:n])
				t.reader.Discard(n)
				return copied, ErrMaxLineSizeExceeded
			}

			if len(b) < idx {
				return 0, ErrShortBuffer
			}

			copied := copy(b, peeked[:idx])
			t.reader.Discard(idx + 1)
			return copied, nil

		case stateEOF:
			if !t.cfg.Follow {
				n := t.reader.Buffered()
				if n > 0 {
					// Probe for more data
					_, peekErr := t.reader.Peek(n + 1)
					if errors.Is(peekErr, io.EOF) {
						// True EOF
						// Return as final line
						peeked, _ := t.reader.Peek(n)
						copied := copy(b, peeked[:n])
						t.reader.Discard(n)
						return copied, nil
					}
					if peekErr != nil {
						t.failErr = peekErr
						t.state = stateFailed
						return 0, peekErr
					}
					// Has more
					t.state = stateReady
					continue
				}
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
			case ChangeModified, ChangeCreated:
				// Drain EOF error in bufio.Reader
				if n := t.reader.Buffered(); n > 0 && n < t.cfg.MaxLineSize {
					t.reader.Peek(n + 1) // trigger file read
				}
				t.state = stateReady
				continue
			case ChangeDeleted:
				// Drain buffer from old file
				if n := t.reader.Buffered(); n > 0 {
					if len(b) < n {
						return 0, ErrShortBuffer
					}
					peeked, _ := t.reader.Peek(n)
					copied := copy(b, peeked[:n])
					t.reader.Discard(n)
					t.file.Close()
					t.file = nil
					t.state = stateNotOpen
					return copied, nil
				}
				err = t.file.Close()
				if err != nil {
					t.failErr = err
					t.state = stateFailed
					return 0, err
				}
				t.file = nil
				t.state = stateNotOpen
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
			case ChangeCreated, ChangeModified, ChangeTruncated:
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
