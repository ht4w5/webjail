package transport

import (
	"context"
	"errors"
	"io"

	"github.com/HT4w5/webjail/pkg/tail"
)

type TailConfig interface {
	Path() string
	NoSeek() bool
}

var _ Transport = (*Tail)(nil)

type Tail struct {
	tail *tail.Tail
}

func NewTail(cfg TailConfig) (*Tail, error) {
	tail, err := tail.New(context.TODO(), cfg.Path(), tail.Config{
		Seek: tail.SeekInfo{
			Offset: 0,
			Whence: io.SeekEnd,
		},
		Follow:      true,
		Retry:       true,
		NoSeek:      cfg.NoSeek(),
		MaxLineSize: 4096,
		Delimiter:   '\n',
	})
	if err != nil {
		return nil, err
	}
	return &Tail{tail: tail}, nil
}

func (t *Tail) Read(b []byte) (int, error) {
	return t.tail.ReadLine(b)
}

func (t *Tail) Close() error {
	if err := t.tail.Close(); errors.Is(err, tail.ErrClosed) {
		return ErrClosed
	} else {
		return err
	}
}
