package filesize

import (
	"context"
	"crypto/sha256"
	"math/rand/v2"
	"unsafe"
)

var _ Querier = (*Mock)(nil)

type Mock struct{}

func (m *Mock) Query(ctx context.Context, url string) (size int64, err error) {
	rnd := rand.New(rand.NewChaCha8(sha256.Sum256(unsafe.Slice(unsafe.StringData(url), len(url)))))
	// 10 in 1 chance of no size info
	if rnd.IntN(10) == 0 {
		return NoInfo, ErrNoInfo
	}
	return rnd.Int64(), nil
}
