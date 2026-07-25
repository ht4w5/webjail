package transport

import (
	"errors"
	"fmt"
)

var (
	ErrClosed = errors.New("transport closed")
)

type Method int8

const (
	MethodUDP Method = iota
	MethodUnixgram
	MethodTail
)

type Config interface {
	Method() Method
	UDP() UDPConfig
	Unixgram() UnixgramConfig
	Tail() TailConfig
}

type Transport interface {
	Read(b []byte) (int, error)
	Close() error
}

func New(cfg Config) (Transport, error) {
	switch cfg.Method() {
	case MethodUDP:
		return NewUDP(cfg.UDP())
	case MethodUnixgram:
		return NewUnixgram(cfg.Unixgram())
	case MethodTail:
		return NewTail(cfg.Tail())
	default:
		return nil, fmt.Errorf("New: unknown method %d", cfg.Method())
	}
}
