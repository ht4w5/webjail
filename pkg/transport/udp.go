package transport

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
)

type UDPConfig interface {
	Listen() netip.AddrPort
}

var _ Transport = (*UDP)(nil)

type UDP struct {
	conn *net.UDPConn
}

func NewUDP(cfg UDPConfig) (*UDP, error) {
	addr := cfg.Listen()
	conn, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(addr))
	if err != nil {
		return nil, fmt.Errorf("NewUDPListener: listen on udp://%s failed: %w", addr.String(), err)
	}

	return &UDP{conn: conn}, nil
}

func (ul *UDP) Read(b []byte) (int, error) {
	return ul.conn.Read(b)
}

func (ul *UDP) Close() error {
	if err := ul.conn.Close(); errors.Is(err, net.ErrClosed) {
		return ErrClosed
	} else {
		return err
	}
}
