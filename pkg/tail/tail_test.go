// LLM usage: generated with deepseek-v4-pro and manually modified.
package tail

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// fakeWatcher is a channel-based Watcher for deterministic testing.
// Tests push Change events via send() to control when BlockUntilChange returns.
type fakeWatcher struct {
	ch chan Change
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{ch: make(chan Change, 10)}
}

func (f *fakeWatcher) BlockUntilChange(ctx context.Context) (Change, error) {
	select {
	case c := <-f.ch:
		return c, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (f *fakeWatcher) Close() error { return nil }

func (f *fakeWatcher) send(c Change) {
	f.ch <- c
}

// startReadLine launches ReadLine in a goroutine and returns a function that
// waits up to 5 seconds for the result. Callers should trigger the necessary
// events (e.g., fw.send) between calling startReadLine and calling the
// returned wait function.
func startReadLine(t testing.TB, tail *Tail, buf []byte) func() (int, error) {
	t.Helper()

	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := tail.ReadLine(buf)
		ch <- result{n: n, err: err}
	}()

	return func() (int, error) {
		select {
		case r := <-ch:
			return r.n, r.err
		case <-time.After(5 * time.Second):
			t.Fatalf("startReadLine: timeout waiting for ReadLine")
			return 0, nil
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 1: Tracer bullet — Seek to start, no Follow
// ---------------------------------------------------------------------------

func TestTail_SeekToStart_NoFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte("line1\nline2\nline3\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Seek:      SeekInfo{Offset: 0, Whence: io.SeekStart},
		Follow:    false,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// First read: ReadLine blocks until file change event arrives.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if err != nil {
		t.Fatalf("line 1: unexpected error: %v", err)
	}
	if string(buf[:n]) != "line1" {
		t.Fatalf("line 1: got %q, want %q", string(buf[:n]), "line1")
	}

	// Subsequent reads: data already buffered, return synchronously.
	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("line 2: unexpected error: %v", err)
	}
	if string(buf[:n]) != "line2" {
		t.Fatalf("line 2: got %q, want %q", string(buf[:n]), "line2")
	}

	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("line 3: unexpected error: %v", err)
	}
	if string(buf[:n]) != "line3" {
		t.Fatalf("line 3: got %q, want %q", string(buf[:n]), "line3")
	}

	// EOF: no more data, Follow=false → io.EOF.
	n, err = tail.ReadLine(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("EOF: expected io.EOF, got n=%d, err=%v", n, err)
	}
	if n != 0 {
		t.Fatalf("EOF: expected n=0, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Phase 2: Seek to end + Follow — only new data returned
// ---------------------------------------------------------------------------

func TestTail_SeekToEnd_Follow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte("old_data\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Seek:      SeekInfo{Offset: 0, Whence: io.SeekEnd},
		Follow:    true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Start ReadLine — it blocks after seeking to end of file.
	wait := startReadLine(t, tail, buf)

	time.Sleep(50 * time.Millisecond)

	// Append new data and notify.
	appendFile(t, path, []byte("new_line\n"))
	fw.send(ChangeModified)

	n, err := wait()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(buf[:n]) != "new_line" {
		t.Fatalf("got %q, want %q", string(buf[:n]), "new_line")
	}
}

// ---------------------------------------------------------------------------
// Phase 3: Recovery tests
// ---------------------------------------------------------------------------

func TestTail_RetryOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// File does not exist yet.

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Retry:     true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Start ReadLine — blocks waiting for file.
	wait := startReadLine(t, tail, buf)

	time.Sleep(50 * time.Millisecond)

	// Attempt 1: file still missing → openFile fails → retry.
	fw.send(ChangeCreated)
	time.Sleep(50 * time.Millisecond)

	// Now create the file and notify again.
	writeFile(t, path, []byte("hello\n"))
	fw.send(ChangeCreated)

	n, err := wait()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("got %q, want %q", string(buf[:n]), "hello")
	}
}

func TestTail_FileDeletedWhileFollowing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte("first\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Retry:     true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Initial read.
	n, err := tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("first line: unexpected error: %v", err)
	}
	if string(buf[:n]) != "first" {
		t.Fatalf("first line: got %q, want %q", string(buf[:n]), "first")
	}

	// ReadLine again — blocks waiting for more data.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	// Delete the file.
	if err := os.Remove(path); err != nil {
		t.Fatalf("os.Remove: %v", err)
	}
	fw.send(ChangeDeleted)
	time.Sleep(50 * time.Millisecond)

	// Recreate with new content.
	writeFile(t, path, []byte("second\n"))
	fw.send(ChangeCreated)

	n, err = wait()
	if err != nil {
		t.Fatalf("second line: unexpected error: %v", err)
	}
	if string(buf[:n]) != "second" {
		t.Fatalf("second line: got %q, want %q", string(buf[:n]), "second")
	}
}

// ---------------------------------------------------------------------------
// Phase 4: Edge cases
// ---------------------------------------------------------------------------

func TestTail_EmptyLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte("\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    false,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Read empty line: returns (0, nil).
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if err != nil {
		t.Fatalf("empty line: unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("empty line: expected n=0, got %d", n)
	}

	// Next read: EOF (no more data, Follow=false).
	n, err = tail.ReadLine(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("EOF: expected io.EOF, got n=%d, err=%v", n, err)
	}
	if n != 0 {
		t.Fatalf("EOF: expected n=0, got %d", n)
	}
}

func TestTail_BufferTooShort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte("long_line\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    false,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	// Buffer too small for the line.
	smallBuf := make([]byte, 3)
	wait := startReadLine(t, tail, smallBuf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if !errors.Is(err, ErrBufferTooShort) {
		t.Fatalf("expected ErrBufferTooShort, got n=%d, err=%v", n, err)
	}
	if n != 0 {
		t.Fatalf("expected n=0, got %d", n)
	}

	// Retry with adequate buffer — the line is still in the reader.
	bigBuf := make([]byte, 1024)
	n, err = tail.ReadLine(bigBuf)
	if err != nil {
		t.Fatalf("retry with big buffer: unexpected error: %v", err)
	}
	if string(bigBuf[:n]) != "long_line" {
		t.Fatalf("retry: got %q, want %q", string(bigBuf[:n]), "long_line")
	}
}

func TestTail_TruncationWhileFollowing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte("first_line\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Read initial line.
	n, err := tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("first line: unexpected error: %v", err)
	}
	if string(buf[:n]) != "first_line" {
		t.Fatalf("first line: got %q, want %q", string(buf[:n]), "first_line")
	}

	// ReadLine again — blocks waiting for more data.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	// Truncate the file.
	if err := os.Truncate(path, 0); err != nil {
		t.Fatalf("os.Truncate: %v", err)
	}
	fw.send(ChangeTruncated)
	time.Sleep(50 * time.Millisecond)

	// Write new content and notify.
	writeFile(t, path, []byte("after_trunc\n"))
	fw.send(ChangeModified)

	n, err = wait()
	if err != nil {
		t.Fatalf("after trunc: unexpected error: %v", err)
	}
	if string(buf[:n]) != "after_trunc" {
		t.Fatalf("after trunc: got %q, want %q", string(buf[:n]), "after_trunc")
	}
}

func TestTail_NoSeek_Truncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte("before_trunc\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		NoSeek:    true,
		Delimiter: '\n',
	}
	tailCtx, tailCancel := context.WithCancel(context.Background())
	defer tailCancel()
	tail := newTailWithWatcher(tailCtx, path, cfg, fw)

	buf := make([]byte, 1024)

	// Read initial line.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if err != nil {
		tail.Close()
		t.Fatalf("first line: unexpected error: %v", err)
	}
	if string(buf[:n]) != "before_trunc" {
		tail.Close()
		t.Fatalf("first line: got %q, want %q", string(buf[:n]), "before_trunc")
	}

	// ReadLine again — blocks.
	wait = startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	// Truncate: with NoSeek=true, the file pointer is NOT reset to 0.
	if err := os.Truncate(path, 0); err != nil {
		tail.Close()
		t.Fatalf("os.Truncate: %v", err)
	}
	fw.send(ChangeTruncated)
	time.Sleep(50 * time.Millisecond)

	// Write new content at position 0. The reader is still beyond the file end.
	writeFile(t, path, []byte("new_data\n"))
	fw.send(ChangeModified)

	// Use a short timeout to verify NoSeek: reader is stuck beyond EOF.
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer timeoutCancel()

	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := tail.ReadLine(buf)
		done <- result{n, err}
	}()

	select {
	case r := <-done:
		if r.err == nil && string(buf[:r.n]) == "new_data" {
			tail.Close()
			t.Fatalf("NoSeek=true but new data was read from position 0 (seek occurred)")
		}
		t.Logf("NoSeek result: n=%d, err=%v", r.n, r.err)
	case <-timeoutCtx.Done():
		// Timeout expected. Cancel tail context to unblock ReadLine goroutines,
		// wait for them to finish, then clean up.
		tailCancel()
		<-done
		t.Logf("NoSeek: timed out as expected (reader stuck beyond EOF)")
	}

	tail.Close()
}

// ---------------------------------------------------------------------------
// Phase 4b: Named pipe (mkfifo) tests — the real-world use case for NoSeek
// ---------------------------------------------------------------------------

func TestTail_NamedPipe_NoSeek(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fifo")

	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skipf("mkfifo not supported: %v", err)
	}

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		NoSeek:    true,
		Delimiter: '\n',
	}
	tailCtx, tailCancel := context.WithCancel(context.Background())
	defer tailCancel()
	tail := newTailWithWatcher(tailCtx, path, cfg, fw)

	buf := make([]byte, 1024)

	// Start ReadLine. It blocks on BlockUntilChange waiting for an event.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	// Trigger openFile via ChangeCreated. os.Open on a FIFO blocks until
	// a writer connects on the other end.
	fw.send(ChangeCreated)
	time.Sleep(50 * time.Millisecond)

	// Connect a writer to unblock the reader's os.Open.
	// The writer waits for a signal before writing so we can control
	// when data appears in the pipe.
	writeNow := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Errorf("writer OpenFile: %v", err)
			return
		}
		defer f.Close()
		<-writeNow
		if _, err := f.Write([]byte("pipe_data\n")); err != nil {
			t.Errorf("writer Write: %v", err)
		}
	}()

	// Wait for the pipe to connect and ReadLine to reach BlockUntilChange.
	time.Sleep(100 * time.Millisecond)

	// Signal the writer and notify the reader.
	close(writeNow)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeModified)

	n, err := wait()
	<-writerDone

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(buf[:n]) != "pipe_data" {
		t.Fatalf("got %q, want %q", string(buf[:n]), "pipe_data")
	}

	tail.Close()
}

func TestTail_NamedPipe_NoSeekOff_Fails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fifo")

	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skipf("mkfifo not supported: %v", err)
	}

	fw := newFakeWatcher()

	// NoSeek defaults to false — seeking on a pipe should fail.
	cfg := Config{
		Follow:    true,
		Delimiter: '\n',
	}
	tailCtx, tailCancel := context.WithCancel(context.Background())
	defer tailCancel()
	tail := newTailWithWatcher(tailCtx, path, cfg, fw)

	// Start a writer that opens O_WRONLY (blocks until reader connects).
	// The reader connects when ReadLine's openFile calls os.Open.
	go func() {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		// Hold the pipe open briefly so the reader has time to fail on Seek.
		time.Sleep(500 * time.Millisecond)
		f.Close()
	}()

	buf := make([]byte, 1024)
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	_, err := wait()
	if err == nil {
		tail.Close()
		t.Fatalf("expected error seeking on named pipe, got nil")
	}
	t.Logf("expected error: %v", err)

	tail.Close()
}

// ---------------------------------------------------------------------------
// Phase 5: Shared test suite — runs against both PollWatcher and FSNotifyWatcher
// ---------------------------------------------------------------------------

// TailFactory creates a Tail with a real Watcher for integration testing.
type TailFactory func(t *testing.T, ctx context.Context, filename string, cfg Config) *Tail

// RunTailTests runs the Tail integration suite against a watcher implementation.
func RunTailTests(t *testing.T, factory TailFactory) {
	t.Helper()

	t.Run("seek to start, no follow", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")

		cfg := Config{
			Seek:      SeekInfo{Offset: 0, Whence: io.SeekStart},
			Follow:    false,
			Delimiter: '\n',
		}
		tail := factory(t, context.Background(), path, cfg)
		defer tail.Close()

		// Create file AFTER tail so the watcher detects creation.
		writeFile(t, path, []byte("a\nb\nc\n"))

		buf := make([]byte, 1024)

		n, err := tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("line 1: unexpected error: %v", err)
		}
		if string(buf[:n]) != "a" {
			t.Fatalf("line 1: got %q, want %q", string(buf[:n]), "a")
		}

		n, err = tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("line 2: unexpected error: %v", err)
		}
		if string(buf[:n]) != "b" {
			t.Fatalf("line 2: got %q, want %q", string(buf[:n]), "b")
		}

		n, err = tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("line 3: unexpected error: %v", err)
		}
		if string(buf[:n]) != "c" {
			t.Fatalf("line 3: got %q, want %q", string(buf[:n]), "c")
		}

		n, err = tail.ReadLine(buf)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected io.EOF, got n=%d, err=%v", n, err)
		}
	})

	t.Run("seek to end, follow", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")

		cfg := Config{
			Seek:      SeekInfo{Offset: 0, Whence: io.SeekEnd},
			Follow:    true,
			Delimiter: '\n',
		}
		tail := factory(t, context.Background(), path, cfg)
		defer tail.Close()

		// Create file with old data; tail opens it and seeks to end.
		writeFile(t, path, []byte("old_data\n"))

		buf := make([]byte, 1024)

		// ReadLine blocks after seeking to end (no data yet).
		// Start async, then append to trigger ChangeModified.
		type result struct {
			n   int
			err error
		}
		ch := make(chan result, 1)
		go func() {
			n, err := tail.ReadLine(buf)
			ch <- result{n, err}
		}()

		time.Sleep(100 * time.Millisecond) // let tail open + seek + block
		appendFile(t, path, []byte("new_line\n"))

		r := <-ch
		if r.err != nil {
			t.Fatalf("unexpected error: %v", r.err)
		}
		if string(buf[:r.n]) != "new_line" {
			t.Fatalf("got %q, want %q", string(buf[:r.n]), "new_line")
		}
	})

	t.Run("empty lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")

		cfg := Config{
			Follow:    false,
			Delimiter: '\n',
		}
		tail := factory(t, context.Background(), path, cfg)
		defer tail.Close()

		writeFile(t, path, []byte("\n"))

		buf := make([]byte, 1024)

		n, err := tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("empty line: unexpected error: %v", err)
		}
		if n != 0 {
			t.Fatalf("empty line: expected n=0, got %d", n)
		}

		n, err = tail.ReadLine(buf)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected io.EOF after empty line, got n=%d, err=%v", n, err)
		}
	})
}

func TestTail_PollWatcher(t *testing.T) {
	RunTailTests(t, func(t *testing.T, ctx context.Context, filename string, cfg Config) *Tail {
		w, err := NewPollWatcher(filename, 10*time.Millisecond)
		if err != nil {
			t.Fatalf("NewPollWatcher: %v", err)
		}
		return newTailWithWatcher(ctx, filename, cfg, w)
	})
}

func TestTail_FSNotifyWatcher(t *testing.T) {
	RunTailTests(t, func(t *testing.T, ctx context.Context, filename string, cfg Config) *Tail {
		w, err := NewFSNotifyWatcher(filename)
		if err != nil {
			t.Fatalf("NewFSNotifyWatcher: %v", err)
		}
		return newTailWithWatcher(ctx, filename, cfg, w)
	})
}
