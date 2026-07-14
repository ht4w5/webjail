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

type Tail struct {
	cfg      Config
	filename string
	file     *os.File
	watcher  Watcher

	reader *bufio.Reader

	firstOpen bool

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func New(ctx context.Context, filename string, cfg Config) (*Tail, error) {

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
		cfg:       cfg,
		filename:  filename,
		watcher:   watcher,
		reader:    bufio.NewReader(&fakeReader{}),
		firstOpen: true,
		ctx:       ctx,
		cancel:    cancel,
	}
}

var (
	ErrBufferTooShort = errors.New("buffer is too short to read complete line")
)

func (t *Tail) ReadLine(b []byte) (n int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for {
		// Try to read a complete line already in the buffer
		if t.file != nil {
			n, found, err := t.tryReadLine(b)
			if found || err != nil {
				return n, err
			}
			// No complete line in buffer
			if !t.cfg.Follow {
				return 0, io.EOF
			}
		}

		// Eagerly open file
		if t.file == nil && !t.cfg.Follow {
			if err := t.openFile(); err == nil {
				continue
			}
		}

		// Wait for next file change
		ch, bErr := t.watcher.BlockUntilChange(t.ctx)
		if bErr != nil {
			return 0, bErr
		}

		fileWasOpen := t.file != nil

		if t.file == nil {
			if ch != ChangeDeleted {
				err = t.openFile()
				if err != nil {
					if t.cfg.Retry && t.cfg.Follow {
						continue
					}
					return
				}
			} else {
				continue
			}
		}

		switch ch {
		case ChangeCreated:
			if fileWasOpen {
				err = t.reopenFile()
				if err != nil {
					return
				}
			}
		case ChangeDeleted:
			err = t.closeFile()
			if err != nil {
				return
			}
		case ChangeModified:
			t.reader.Reset(t.file)
		case ChangeTruncated:
			if !t.cfg.NoSeek {
				_, err = t.file.Seek(0, io.SeekStart)
				if err != nil {
					return
				}
			}
		}
	}
}

func (t *Tail) tryReadLine(b []byte) (n int, found bool, err error) {
	nBuf := t.reader.Buffered()
	if nBuf == 0 {
		if _, err := t.reader.Peek(1); err != nil {
			if errors.Is(err, io.EOF) {
				return 0, false, nil
			}
			return 0, false, err
		}
		nBuf = t.reader.Buffered()
	}

	peeked, err := t.reader.Peek(nBuf)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, false, err
	}

	idx := bytes.IndexByte(peeked, t.cfg.Delimiter)
	if idx < 0 {
		return 0, false, nil
	}

	if len(b) < idx {
		return 0, false, ErrBufferTooShort
	}

	copied := copy(b, peeked[:idx])
	t.reader.Discard(idx + 1)
	return copied, true, nil
}

func (t *Tail) Close() error {
	t.cancel()
	return t.closeFile()
}

func (t *Tail) closeFile() (err error) {
	if t.file != nil {
		err = t.file.Close()
		t.file = nil
	}
	return
}

func (t *Tail) openFile() error {
	file, err := os.Open(t.filename)
	if err != nil {
		return err
	}

	if !t.cfg.NoSeek && t.firstOpen {
		if _, err := file.Seek(t.cfg.Seek.Offset, t.cfg.Seek.Whence); err != nil {
			file.Close()
			return err
		}
	}
	t.firstOpen = false

	t.file = file
	t.reader.Reset(t.file)
	return nil
}

func (t *Tail) reopenFile() (err error) {
	err = t.closeFile()
	if err != nil {
		return
	}
	err = t.openFile()
	return
}
