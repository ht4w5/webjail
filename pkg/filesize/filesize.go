package filesize

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/HT4w5/webjail/pkg/log"
	"github.com/rs/zerolog"
)

const (
	NoInfo int64 = -1
)

var (
	ErrNoInfo = errors.New("no size info")
	ErrBadURL = errors.New("bad url")
)

type Querier interface {
	Query(ctx context.Context, url string) (size int64, err error)
}

type Method int8

const (
	MethodMock Method = iota
	MethodLocal
)

type Config interface {
	EnableCache() (ok bool, ttl time.Duration, maxBytes int)
	Method() Method
	Local() LocalConfig
}

type Manager struct {
	src    Querier
	logger zerolog.Logger
}

var _ Querier = (*Manager)(nil)

func NewManager(cfg Config, logger zerolog.Logger) (*Manager, error) {
	var src Querier
	switch cfg.Method() {
	case MethodMock:
		src = &Mock{}
	case MethodLocal:
		src = NewLocal(cfg.Local(), logger)
	default:
		return nil, fmt.Errorf("NewManager: unknown method %d", cfg.Method())
	}

	if ok, ttl, maxBytes := cfg.EnableCache(); ok {
		src = NewCache(src, ttl, maxBytes, logger)
	}

	mgr := &Manager{
		src:    src,
		logger: logger.With().Str(log.KeyModule, "filesize.Manager").Logger(),
	}

	return mgr, nil
}

func (m *Manager) Query(ctx context.Context, url string) (size int64, err error) {
	size, err = m.src.Query(ctx, url)
	if err != nil && !errors.Is(err, ErrNoInfo) {
		m.logger.Warn().Stack().Err(err).Str("url", url).Msg("query error")
	}
	m.logger.Info().Str("url", url).Int64("size", size).Msg("query result")
	return
}
