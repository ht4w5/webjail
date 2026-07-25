package ingest

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/HT4w5/webjail/pkg/log"
	"github.com/HT4w5/webjail/pkg/parser"
	"github.com/HT4w5/webjail/pkg/request"
	"github.com/HT4w5/webjail/pkg/transport"
	"github.com/rs/zerolog"
)

type Ingest interface {
	Start(ctx context.Context, reqChan chan<- *request.Request) error
	Shutdown(ctx context.Context) error
}

type Config interface {
	Sources() []SourceConfig
}

type SourceConfig interface {
	Transport() transport.Config
	Parser() parser.Config
}

var _ Ingest = (*Manager)(nil)

type Manager struct {
	sources []source
	workers struct {
		sync.WaitGroup
		ctx    context.Context
		cancel context.CancelFunc
	}
	logger zerolog.Logger
}

type source struct {
	parser    parser.Parser
	transport transport.Transport
}

func NewManager(cfg Config, logger zerolog.Logger) (*Manager, error) {
	srcCfgs := cfg.Sources()

	mgr := &Manager{
		sources: make([]source, 0, len(srcCfgs)),
		logger:  logger.With().Str(log.KeyModule, "ingest.Manager").Logger(),
	}

	for _, cfg := range srcCfgs {
		p, err := parser.New(cfg.Parser())
		if err != nil {
			return nil, fmt.Errorf("NewManager: create parser: %w", err)
		}

		t, err := transport.New(cfg.Transport())
		if err != nil {
			return nil, fmt.Errorf("NewManager: create transport: %w", err)
		}

		mgr.sources = append(mgr.sources, source{
			parser:    p,
			transport: t,
		})
	}

	return mgr, nil
}

func (mgr *Manager) Start(ctx context.Context, reqChan chan<- *request.Request) error {
	mgr.workers.ctx, mgr.workers.cancel = context.WithCancel(context.Background())

	for _, src := range mgr.sources {
		mgr.workers.Go(func() {
			<-mgr.workers.ctx.Done()
			if err := src.transport.Close(); err != nil {
				mgr.logger.Warn().Stack().Err(err).Msg("close transport failed")
			}
		})
		mgr.workers.Go(func() {
			t := src.transport
			p := src.parser
			for {
				req := request.Get()
				buf := req.Bytes()
				n, err := t.Read(buf)
				if err != nil {
					if errors.Is(err, transport.ErrClosed) {
						request.Put(req)
						return
					} else {
						request.Put(req)
						mgr.logger.Error().Stack().Err(err).Msg("read from transport failed")
						continue
					}
				}

				if err := p.Parse(buf[:n], req); err != nil {
					request.Put(req)
					mgr.logger.Warn().Stack().Err(err).Msg("parse request failed")
					continue
				}

				reqChan <- req
				mgr.logger.Info().Object(log.KeyRequest, req).Send()
			}
		})
	}

	return nil
}

func (mgr *Manager) Shutdown(ctx context.Context) error {
	mgr.workers.cancel()

	done := make(chan struct{}, 1)

	go func() {
		mgr.workers.Wait()
		done <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
