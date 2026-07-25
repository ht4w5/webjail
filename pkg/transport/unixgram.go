package transport

import (
	"errors"
	"fmt"
	"net"
)

type UnixgramConfig interface {
	Listen() string
}

var _ Transport = (*Unixgram)(nil)

type Unixgram struct {
	conn *net.UnixConn
}

func NewUnixgram(cfg UnixgramConfig) (*Unixgram, error) {
	addr := cfg.Listen()
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{
		Name: addr,
		Net:  "unixgram",
	})
	if err != nil {
		return nil, fmt.Errorf("NewUnixgramListener: listen on unixgram://%s failed: %w", addr, err)
	}

	return &Unixgram{conn: conn}, nil
}

func (ul *Unixgram) Read(b []byte) (int, error) {
	return ul.conn.Read(b)
}

func (ul *Unixgram) Close() error {
	if err := ul.conn.Close(); errors.Is(err, net.ErrClosed) {
		return ErrClosed
	} else {
		return err
	}
}
