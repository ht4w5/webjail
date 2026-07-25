package filesize

import (
	"context"
	"errors"
	"os"
	"path"

	"github.com/HT4w5/webjail/pkg/log"
	"github.com/HT4w5/webjail/pkg/trie"
	"github.com/rs/zerolog"
)

type LocalConfig interface {
	Routes() map[string]string
}

var _ Querier = (*Local)(nil)

// Context cancellation is NOT respected in the Local Querier.
// Probably bad for high-latency filesystems.
type Local struct {
	trie   trie.PrefixTrie[string]
	logger zerolog.Logger
}

func NewLocal(cfg LocalConfig, logger zerolog.Logger) *Local {
	tb := trie.NewPrefixTrieBuilder[string]()

	for k, v := range cfg.Routes() {
		tb.Add(k, v)
	}

	return &Local{
		trie:   tb.Build(),
		logger: logger.With().Str(log.KeyModule, "filesize.Local").Logger(),
	}
}

func (l *Local) Query(ctx context.Context, url string) (int64, error) {
	url = path.Clean(url)
	if url == "" || url[0] != '/' {
		l.logger.Warn().Str("url", url).Msg("bad query url")
		return 0, ErrBadURL
	}
	newPrefix, prefixLen, ok := l.trie.LongestPrefixMatchWithLen(url)
	if !ok {
		return NoInfo, ErrNoInfo
	}

	return l.queryFilesystem(newPrefix + url[prefixLen:])
}

func (l *Local) queryFilesystem(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NoInfo, ErrNoInfo
		}
		l.logger.Warn().Stack().Err(err).Msg("stat file error")
		return 0, err
	}
	if info.IsDir() {
		return NoInfo, ErrNoInfo
	}
	return info.Size(), nil
}
