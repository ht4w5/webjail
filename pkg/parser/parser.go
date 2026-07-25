package parser

import (
	"fmt"

	"github.com/HT4w5/webjail/pkg/request"
)

type Format int8

const (
	FormatNginx Format = iota
)

type Config interface {
	Format() Format
}

type Parser interface {
	Parse(b []byte, req *request.Request) error
}

func New(cfg Config) (Parser, error) {
	switch cfg.Format() {
	case FormatNginx:
		return NewNginxParser(), nil
	default:
		return nil, fmt.Errorf("New: unknown format %d", cfg.Format())
	}
}
