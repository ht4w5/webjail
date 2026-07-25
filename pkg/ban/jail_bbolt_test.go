// LLM usage: generated with deepseek-v4-pro and manually modified.
package ban

import (
	"context"
	"encoding/binary"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.etcd.io/bbolt"
)

// newTestJail creates a BBoltJail with a temp file, ready for testing.
// The caller is responsible for calling Shutdown and removing the file.
func newTestJail(t *testing.T, retention time.Duration, start bool) (*BBoltJail, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "jail.db")

	j, err := NewBBoltJail(path, retention, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewBBoltJail failed: %v", err)
	}

	if start {
		if err := j.Start(context.Background()); err != nil {
			j.Shutdown(context.Background())
			t.Fatalf("Start failed: %v", err)
		}
		t.Cleanup(func() {
			j.Shutdown(context.Background())
		})
	}

	return j, path
}

func mustIP(s string) netip.Addr {
	return netip.MustParseAddr(s)
}

func TestBBoltJail_New_InvalidRetention(t *testing.T) {
	_, err := NewBBoltJail(filepath.Join(t.TempDir(), "jail.db"), -1*time.Second, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error for negative retention")
	}
}

func TestBBoltJail_New_ZeroRetention(t *testing.T) {
	j, err := NewBBoltJail(filepath.Join(t.TempDir(), "jail.db"), 0, zerolog.Nop())
	if err != nil {
		t.Fatalf("expected success for zero retention: %v", err)
	}
	j.Shutdown(context.Background())
}

func TestBBoltJail_AddGet_IPv4(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	rec := &Record{
		Addr:      mustIP("192.168.1.1"),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
		Blame:     "test-ipv4",
	}

	if err := j.Add(ctx, rec); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := j.Get(ctx, rec.Addr)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Addr != rec.Addr {
		t.Errorf("Addr mismatch: got %v, want %v", got.Addr, rec.Addr)
	}
	if !got.CreatedAt.Equal(rec.CreatedAt) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", got.CreatedAt, rec.CreatedAt)
	}
	if !got.ExpiresAt.Equal(rec.ExpiresAt) {
		t.Errorf("ExpiresAt mismatch: got %v, want %v", got.ExpiresAt, rec.ExpiresAt)
	}
	if got.Blame != rec.Blame {
		t.Errorf("Blame mismatch: got %q, want %q", got.Blame, rec.Blame)
	}
}

func TestBBoltJail_AddGet_IPv6(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	rec := &Record{
		Addr:      mustIP("::1"),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
		Blame:     "test-ipv6",
	}

	if err := j.Add(ctx, rec); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := j.Get(ctx, rec.Addr)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Addr != rec.Addr {
		t.Errorf("Addr mismatch: got %v, want %v", got.Addr, rec.Addr)
	}
	if got.Blame != rec.Blame {
		t.Errorf("Blame mismatch: got %q, want %q", got.Blame, rec.Blame)
	}
}

func TestBBoltJail_Get_NotFound(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	_, err := j.Get(ctx, mustIP("10.0.0.1"))
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBBoltJail_Add_Overwrite(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()
	addr := mustIP("10.0.0.1")

	r1 := &Record{Addr: addr, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(), Blame: "first"}
	r2 := &Record{Addr: addr, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(2 * time.Hour).UTC(), Blame: "second"}

	if err := j.Add(ctx, r1); err != nil {
		t.Fatalf("Add r1 failed: %v", err)
	}
	if err := j.Add(ctx, r2); err != nil {
		t.Fatalf("Add r2 failed: %v", err)
	}

	got, err := j.Get(ctx, addr)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Blame != "second" {
		t.Errorf("expected overwritten value 'second', got %q", got.Blame)
	}
}

func TestBBoltJail_Del(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()
	addr := mustIP("10.0.0.1")

	rec := &Record{Addr: addr, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(), Blame: "test"}
	if err := j.Add(ctx, rec); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := j.Del(ctx, addr); err != nil {
		t.Fatalf("Del failed: %v", err)
	}

	_, err := j.Get(ctx, addr)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after Del, got %v", err)
	}
}

func TestBBoltJail_Del_NonExistent(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	if err := j.Del(ctx, mustIP("10.0.0.99")); err != nil {
		t.Fatalf("Del non-existent should succeed: %v", err)
	}
}

func TestBBoltJail_ForEach_Empty(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	count := 0
	err := j.ForEach(ctx, mustIP("0.0.0.0"), mustIP("255.255.255.255"), func(r *Record) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 records, got %d", count)
	}
}

func TestBBoltJail_ForEach_Single(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	rec := &Record{Addr: mustIP("10.0.0.5"), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(), Blame: "test"}
	if err := j.Add(ctx, rec); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	var records []Record
	err := j.ForEach(ctx, mustIP("10.0.0.0"), mustIP("10.0.0.255"), func(r *Record) error {
		records = append(records, *r)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Addr != rec.Addr {
		t.Errorf("Addr mismatch: got %v, want %v", records[0].Addr, rec.Addr)
	}
}

func TestBBoltJail_ForEach_RangeBounds(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	ips := []string{"10.0.0.1", "10.0.0.5", "10.0.0.10", "10.0.1.1", "192.168.1.1"}
	for _, ip := range ips {
		rec := &Record{Addr: mustIP(ip), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(), Blame: ip}
		if err := j.Add(ctx, rec); err != nil {
			t.Fatalf("Add %s failed: %v", ip, err)
		}
	}

	tests := []struct {
		name  string
		start string
		end   string
		want  int
	}{
		{"full range", "10.0.0.0", "10.0.1.255", 4},
		{"narrow range", "10.0.0.0", "10.0.0.5", 2},
		{"single match", "192.168.1.1", "192.168.1.1", 1},
		{"no match", "20.0.0.0", "20.0.0.255", 0},
		{"ipv6 empty", "::1", "::9", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := 0
			err := j.ForEach(ctx, mustIP(tt.start), mustIP(tt.end), func(r *Record) error {
				count++
				return nil
			})
			if err != nil {
				t.Fatalf("ForEach failed: %v", err)
			}
			if count != tt.want {
				t.Errorf("expected %d records, got %d", tt.want, count)
			}
		})
	}
}

func TestBBoltJail_ForEach_CallbackError(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		rec := &Record{Addr: mustIP(ip), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(), Blame: ip}
		j.Add(ctx, rec)
	}

	stopErr := os.ErrExist
	count := 0
	err := j.ForEach(ctx, mustIP("10.0.0.0"), mustIP("10.0.0.255"), func(r *Record) error {
		count++
		if count >= 2 {
			return stopErr
		}
		return nil
	})
	if err != stopErr {
		t.Fatalf("expected %v, got %v", stopErr, err)
	}
	if count != 2 {
		t.Errorf("expected 2 callbacks, got %d", count)
	}
}

func TestBBoltJail_ForEach_CancelledContext(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		addr := netip.AddrFrom4([4]byte{10, 0, 0, byte(i + 1)})
		rec := &Record{Addr: addr, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(), Blame: "x"}
		j.Add(ctx, rec)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // cancel immediately

	err := j.ForEach(cancelCtx, mustIP("10.0.0.0"), mustIP("10.0.0.255"), func(r *Record) error {
		return nil
	})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestBBoltJail_ConcurrentAddGet(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)
	ctx := context.Background()

	var wg sync.WaitGroup
	n := 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := netip.AddrFrom4([4]byte{10, 0, 0, byte(idx + 1)})
			rec := &Record{Addr: addr, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(), Blame: "concurrent"}
			if err := j.Add(ctx, rec); err != nil {
				t.Errorf("Add %v failed: %v", addr, err)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		addr := netip.AddrFrom4([4]byte{10, 0, 0, byte(i + 1)})
		_, err := j.Get(ctx, addr)
		if err != nil {
			t.Fatalf("Get %v failed: %v", addr, err)
		}
	}
}

func TestBBoltJail_Shutdown_Timeout(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)

	// Shutdown with an already-cancelled context should timeout.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := j.Shutdown(cancelCtx)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestBBoltJail_CleanupExpired(t *testing.T) {
	j, _ := newTestJail(t, 0, true) // retention=0: delete immediately on expiry
	ctx := context.Background()

	// Add an already-expired record.
	rec := &Record{
		Addr:      mustIP("10.0.0.1"),
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC(),
		ExpiresAt: time.Now().Add(-1 * time.Hour).UTC(), // expired
		Blame:     "expired",
	}
	if err := j.Add(ctx, rec); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Add a non-expired record.
	active := &Record{
		Addr:      mustIP("10.0.0.2"),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
		Blame:     "active",
	}
	if err := j.Add(ctx, active); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Manually trigger cleanup.
	j.cleanupExpired()

	_, err := j.Get(ctx, rec.Addr)
	if err != ErrNotFound {
		t.Errorf("expected expired record to be deleted, got err=%v", err)
	}

	got, err := j.Get(ctx, active.Addr)
	if err != nil {
		t.Fatalf("expected active record to remain: %v", err)
	}
	if got.Blame != "active" {
		t.Errorf("Blame mismatch: got %q, want 'active'", got.Blame)
	}
}

func TestBBoltJail_CleanupRespectsRetention(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true) // retention=1h: keep for 1h after expiry
	ctx := context.Background()

	// Add a record that expired 30 minutes ago — within retention window.
	rec := &Record{
		Addr:      mustIP("10.0.0.1"),
		CreatedAt: time.Now().Add(-90 * time.Minute).UTC(),
		ExpiresAt: time.Now().Add(-30 * time.Minute).UTC(),
		Blame:     "still-retained",
	}
	if err := j.Add(ctx, rec); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	j.cleanupExpired()

	_, err := j.Get(ctx, rec.Addr)
	if err != nil {
		t.Fatalf("record within retention should not be deleted: %v", err)
	}
}

func TestBBoltJail_RecordKey_IPv4(t *testing.T) {
	addr := mustIP("1.2.3.4")
	var kbuf [16]byte
	key := appendKey(kbuf[:0], addr)
	if len(key) != 4 {
		t.Fatalf("expected 4-byte key, got %d", len(key))
	}
	got, err := addrFromKey(key)
	if err != nil {
		t.Fatalf("addrFromKey failed: %v", err)
	}
	if got != addr {
		t.Errorf("round-trip failed: got %v, want %v", got, addr)
	}
}

func TestBBoltJail_RecordKey_IPv6(t *testing.T) {
	addr := mustIP("2001:db8::1")
	var kbuf [16]byte
	key := appendKey(kbuf[:0], addr)
	if len(key) != 16 {
		t.Fatalf("expected 16-byte key, got %d", len(key))
	}
	got, err := addrFromKey(key)
	if err != nil {
		t.Fatalf("addrFromKey failed: %v", err)
	}
	if got != addr {
		t.Errorf("round-trip failed: got %v, want %v", got, addr)
	}
}

func TestBBoltJail_RecordKey_InvalidLength(t *testing.T) {
	_, err := addrFromKey([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for 3-byte key")
	}
}

func TestBBoltJail_EncodeDecode_BlameEmpty(t *testing.T) {
	addr := mustIP("10.0.0.1")
	rec := &Record{
		Addr:      addr,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
		Blame:     "",
	}
	data := encodeRecord(nil, rec)
	got, err := decodeRecord(addr, data)
	if err != nil {
		t.Fatalf("decodeRecord failed: %v", err)
	}
	if got.Blame != "" {
		t.Errorf("expected empty Blame, got %q", got.Blame)
	}
}

func TestBBoltJail_Decode_InvalidData(t *testing.T) {
	addr := mustIP("10.0.0.1")

	_, err := decodeRecord(addr, []byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short data")
	}

	// 18 bytes with BlameLen=5 but no blame bytes
	data := make([]byte, 18)
	binary.LittleEndian.PutUint16(data[16:18], 5)
	_, err = decodeRecord(addr, data)
	if err == nil {
		t.Fatal("expected error for mismatched BlameLen")
	}
}

// Test that BBoltJail satisfies the Jail interface at compile time.
var _ Jail = (*BBoltJail)(nil)

func TestBBoltJail_DatabaseFilePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jail.db")

	addr := mustIP("10.0.0.1")
	rec := &Record{Addr: addr, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(), Blame: "persist"}

	// Create and write.
	j1, err := NewBBoltJail(path, time.Hour, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewBBoltJail failed: %v", err)
	}
	if err := j1.Start(context.Background()); err != nil {
		j1.Shutdown(context.Background())
		t.Fatalf("Start failed: %v", err)
	}
	if err := j1.Add(context.Background(), rec); err != nil {
		j1.Shutdown(context.Background())
		t.Fatalf("Add failed: %v", err)
	}
	if err := j1.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Reopen and read.
	j2, err := NewBBoltJail(path, time.Hour, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewBBoltJail (reopen) failed: %v", err)
	}
	defer j2.Shutdown(context.Background())

	if err := j2.Start(context.Background()); err != nil {
		t.Fatalf("Start (reopen) failed: %v", err)
	}

	got, err := j2.Get(context.Background(), addr)
	if err != nil {
		t.Fatalf("Get after reopen failed: %v", err)
	}
	if got.Blame != "persist" {
		t.Errorf("Blame mismatch after reopen: got %q, want 'persist'", got.Blame)
	}
}

// Ensure bbolt.BucketStats is accessible via Stats().
func TestBBoltJail_Stats(t *testing.T) {
	j, _ := newTestJail(t, time.Hour, true)

	s := j.Stats()
	if s == nil {
		t.Fatal("Stats returned nil")
	}
	// j.db.Stats() returns bbolt.Stats which is non-nil
	if _, ok := s.(bbolt.Stats); !ok {
		t.Errorf("expected bbolt.Stats, got %T", s)
	}
}
