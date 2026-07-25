// LLM usage: generated with deepseek-v4-pro and manually modified.
package filesize_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HT4w5/webjail/pkg/filesize"
	"github.com/rs/zerolog"
)

// mockQuerier is a controllable Querier for testing.
type mockQuerier struct {
	size int64
	err  error
}

func (m *mockQuerier) Query(_ context.Context, _ string) (int64, error) {
	return m.size, m.err
}

// countingQuerier wraps a Querier and counts Query calls.
type countingQuerier struct {
	upstream filesize.Querier
	calls    atomic.Int64
}

func (c *countingQuerier) Query(ctx context.Context, url string) (int64, error) {
	c.calls.Add(1)
	return c.upstream.Query(ctx, url)
}

func (c *countingQuerier) Calls() int64 {
	return c.calls.Load()
}

func TestCache_Hit(t *testing.T) {
	mock := &mockQuerier{size: 42}
	counter := &countingQuerier{upstream: mock}

	cache := filesize.NewCache(counter, 1*time.Second, 1<<20, zerolog.Nop())
	ctx := context.Background()

	url := "/test-hit"

	// First call: upstream must be queried.
	size1, err1 := cache.Query(ctx, url)
	callsAfterFirst := counter.Calls()
	if callsAfterFirst != 1 {
		t.Fatalf("expected 1 upstream call after first query, got %d", callsAfterFirst)
	}

	// Second call: must hit cache, no upstream call.
	size2, err2 := cache.Query(ctx, url)
	callsAfterSecond := counter.Calls()
	if callsAfterSecond != 1 {
		t.Fatalf("expected 1 upstream call after second query (cache hit), got %d", callsAfterSecond)
	}

	// Results must match.
	if size1 != size2 || size1 != 42 {
		t.Fatalf("cache returned unexpected result: size1=%d size2=%d", size1, size2)
	}
	if err1 != nil || err2 != nil {
		t.Fatalf("cache returned error: err1=%v err2=%v", err1, err2)
	}
}

func TestCache_ErrNoInfo_Cached(t *testing.T) {
	mock := &mockQuerier{size: -1, err: filesize.ErrNoInfo}
	counter := &countingQuerier{upstream: mock}

	cache := filesize.NewCache(counter, 10*time.Second, 1<<20, zerolog.Nop())
	ctx := context.Background()

	url := "/noinfo"

	// First call: upstream queried, ErrNoInfo returned.
	_, err1 := cache.Query(ctx, url)
	if counter.Calls() != 1 {
		t.Fatalf("expected 1 upstream call after first query, got %d", counter.Calls())
	}
	if !errors.Is(err1, filesize.ErrNoInfo) {
		t.Fatalf("expected ErrNoInfo, got %v", err1)
	}

	// Second call: must hit cache, same ErrNoInfo, no upstream call.
	_, err2 := cache.Query(ctx, url)
	if counter.Calls() != 1 {
		t.Fatalf("expected 1 upstream call after second query (cached ErrNoInfo), got %d", counter.Calls())
	}
	if !errors.Is(err2, filesize.ErrNoInfo) {
		t.Fatalf("expected ErrNoInfo on second call, got %v", err2)
	}
}

func TestCache_TTL_Expiry(t *testing.T) {
	mock := &mockQuerier{size: 42}
	counter := &countingQuerier{upstream: mock}

	cache := filesize.NewCache(counter, 10*time.Millisecond, 1<<20, zerolog.Nop())
	ctx := context.Background()

	url := "/ttl-test"

	// First call: upstream queried.
	cache.Query(ctx, url)
	if counter.Calls() != 1 {
		t.Fatalf("expected 1 upstream call after first query, got %d", counter.Calls())
	}

	// Wait for TTL to expire.
	time.Sleep(20 * time.Millisecond)

	// Second call: entry expired, upstream queried again.
	cache.Query(ctx, url)
	if counter.Calls() != 2 {
		t.Fatalf("expected 2 upstream calls after TTL expiry, got %d", counter.Calls())
	}
}

func TestCache_ErrBadURL_Passthrough(t *testing.T) {
	mock := &mockQuerier{size: 42}
	counter := &countingQuerier{upstream: mock}

	cache := filesize.NewCache(counter, 1*time.Second, 1<<20, zerolog.Nop())
	ctx := context.Background()

	// Empty URL: Cache returns ErrBadURL directly without querying upstream.
	for range 3 {
		_, err := cache.Query(ctx, "")
		if !errors.Is(err, filesize.ErrBadURL) {
			t.Fatalf("expected ErrBadURL, got %v", err)
		}
	}

	// Cache handles empty URL internally — upstream never called.
	if counter.Calls() != 0 {
		t.Fatalf("expected 0 upstream calls for empty URL (cache handles it), got %d", counter.Calls())
	}

	// Non-empty URL DOES reach upstream (sanity check).
	cache.Query(ctx, "/valid")
	if counter.Calls() != 1 {
		t.Fatalf("expected 1 upstream call for non-empty URL, got %d", counter.Calls())
	}
}
