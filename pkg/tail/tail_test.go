// LLM usage: generated with deepseek-v4-pro and manually modified.
//
// Some tests are ported from github.com/nxadm/tail:
// Copyright (c) 2019 FOSS contributors of https://github.com/nxadm/tail
// Copyright (c) 2015 HPE Software Inc. All rights reserved.
// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.
package tail

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	if !errors.Is(err, ErrShortBuffer) {
		t.Fatalf("expected ErrShortBuffer, got n=%d, err=%v", n, err)
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
// Phase 4c: Ported from nxadm/tail — Close behavior, seek-from-middle, large
//           lines, file rotation, and wait-for-create-then-rename.
// ---------------------------------------------------------------------------

// TestTail_Close_NeverOpened verifies Close() succeeds on a tail that never
// opened a file (port of nxadm/tail TestStop).
func TestTail_Close_NeverOpened(t *testing.T) {
	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Retry:     true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), "/nonexistent/test.log", cfg, fw)

	// Close should succeed without panic.
	if err := tail.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
}

// TestTail_Close_AfterRead verifies Close() succeeds after reading data
// (port of nxadm/tail TestStopNonEmptyFile).
func TestTail_Close_AfterRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte("hello\nthere\nworld\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    false,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Read the first line, then close.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if err != nil {
		t.Fatalf("first line: unexpected error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("first line: got %q, want %q", string(buf[:n]), "hello")
	}

	// Close after reading — must not panic.
	if err := tail.Close(); err != nil {
		t.Fatalf("Close after read: unexpected error: %v", err)
	}
}

// TestTail_Close_WhileBlocked verifies Close() unblocks a ReadLine that is
// waiting for new data (port of nxadm/tail TestStopAtEOF).
func TestTail_Close_WhileBlocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte("hello\nthere\nworld\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Delimiter: '\n',
	}
	tailCtx, tailCancel := context.WithCancel(context.Background())
	defer tailCancel()
	tail := newTailWithWatcher(tailCtx, path, cfg, fw)

	buf := make([]byte, 1024)

	// Read initial lines.
	n, err := tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("line 1: unexpected error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("line 1: got %q, want %q", string(buf[:n]), "hello")
	}

	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("line 2: unexpected error: %v", err)
	}
	if string(buf[:n]) != "there" {
		t.Fatalf("line 2: got %q, want %q", string(buf[:n]), "there")
	}

	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("line 3: unexpected error: %v", err)
	}
	if string(buf[:n]) != "world" {
		t.Fatalf("line 3: got %q, want %q", string(buf[:n]), "world")
	}

	// Now ReadLine blocks waiting for more data (stateEOF → BlockUntilChange).
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	// Close while ReadLine is blocked — must unblock with an error.
	if err := tail.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}

	_, err = wait()
	if err == nil {
		t.Fatalf("expected error after Close, got nil")
	}
	t.Logf("ReadLine unblocked with: %v", err)
}

// TestTail_SeekFromMiddle verifies seeking with a negative offset from
// SeekEnd (port of nxadm/tail TestLocationMiddle).
func TestTail_SeekFromMiddle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// 6 bytes: a\nb\nc\n → seek -4 from end = position 2 → reads "b", "c"
	writeFile(t, path, []byte("a\nb\nc\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Seek:      SeekInfo{Offset: -4, Whence: io.SeekEnd},
		Follow:    true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Read lines from middle.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if err != nil {
		t.Fatalf("line b: unexpected error: %v", err)
	}
	if string(buf[:n]) != "b" {
		t.Fatalf("line b: got %q, want %q", string(buf[:n]), "b")
	}

	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("line c: unexpected error: %v", err)
	}
	if string(buf[:n]) != "c" {
		t.Fatalf("line c: got %q, want %q", string(buf[:n]), "c")
	}

	// Now follow: append new data.
	wait = startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	appendFile(t, path, []byte("d\n"))
	fw.send(ChangeModified)

	n, err = wait()
	if err != nil {
		t.Fatalf("line d: unexpected error: %v", err)
	}
	if string(buf[:n]) != "d" {
		t.Fatalf("line d: got %q, want %q", string(buf[:n]), "d")
	}
}

// TestTail_LargeLine verifies that a line approaching the bufio.Reader's
// default buffer size (4096 bytes) is read correctly when the caller provides
// an adequate buffer (port of nxadm/tail TestOver4096ByteLine).
// NOTE: lines larger than 4096 bytes require MaxLineSize splitting or a
// larger bufio.Reader buffer, neither of which are currently implemented.
func TestTail_LargeLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// Use 4000 bytes — fits within the default 4096-byte bufio.Reader buffer
	// even after the first "small\n" line is consumed.
	largeContent := strings.Repeat("a", 4000)
	writeFile(t, path, []byte("small\n"+largeContent+"\nlast\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	// Buffer large enough for the 4000-byte line.
	buf := make([]byte, 8192)

	// Read small line.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if err != nil {
		t.Fatalf("small line: unexpected error: %v", err)
	}
	if string(buf[:n]) != "small" {
		t.Fatalf("small line: got %q, want %q", string(buf[:n]), "small")
	}

	// Read large line.
	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("large line: unexpected error: %v", err)
	}
	if string(buf[:n]) != largeContent {
		t.Fatalf("large line: got %d bytes, want %d bytes", n, len(largeContent))
	}

	// Read last line.
	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("last line: unexpected error: %v", err)
	}
	if string(buf[:n]) != "last" {
		t.Fatalf("last line: got %q, want %q", string(buf[:n]), "last")
	}
}

// TestTail_RenameThenRecreate verifies that after a file is renamed and a
// new file is created with the same name, tail picks up the new file's
// content (port of nxadm/tail reOpen / TestReOpen*).
func TestTail_RenameThenRecreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	rotated := filepath.Join(dir, "test.log.rotated")
	writeFile(t, path, []byte("hello\nworld\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Retry:     true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Read initial lines.
	n, err := tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("line 1: unexpected error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("line 1: got %q, want %q", string(buf[:n]), "hello")
	}

	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("line 2: unexpected error: %v", err)
	}
	if string(buf[:n]) != "world" {
		t.Fatalf("line 2: got %q, want %q", string(buf[:n]), "world")
	}

	// ReadLine blocks waiting for more data.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	// Rename the file (simulates log rotation).
	if err := os.Rename(path, rotated); err != nil {
		t.Fatalf("os.Rename: %v", err)
	}
	fw.send(ChangeDeleted)
	time.Sleep(50 * time.Millisecond)

	// Create a new file with the same name and new content.
	writeFile(t, path, []byte("newdata\n"))
	fw.send(ChangeCreated)

	n, err = wait()
	if err != nil {
		t.Fatalf("newdata: unexpected error: %v", err)
	}
	if string(buf[:n]) != "newdata" {
		t.Fatalf("newdata: got %q, want %q", string(buf[:n]), "newdata")
	}
}

// TestTail_WaitForCreateThenRename verifies the full lifecycle: file doesn't
// exist → created → read → renamed → recreated → read new content
// (port of nxadm/tail TestInotify_WaitForCreateThenMove).
func TestTail_WaitForCreateThenRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	rotated := filepath.Join(dir, "test.log.rotated")
	// Ensure file does not exist.
	os.Remove(path)

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Retry:     true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Start ReadLine — file doesn't exist, blocks in stateNotOpen.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	// First attempt: file still missing.
	fw.send(ChangeCreated)
	time.Sleep(50 * time.Millisecond)

	// Create file and notify.
	writeFile(t, path, []byte("hello\nworld\n"))
	fw.send(ChangeCreated)

	n, err := wait()
	if err != nil {
		t.Fatalf("hello: unexpected error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("hello: got %q, want %q", string(buf[:n]), "hello")
	}

	// Read second line.
	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("world: unexpected error: %v", err)
	}
	if string(buf[:n]) != "world" {
		t.Fatalf("world: got %q, want %q", string(buf[:n]), "world")
	}

	// Block waiting for more, then rename.
	wait = startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	if err := os.Rename(path, rotated); err != nil {
		t.Fatalf("os.Rename: %v", err)
	}
	fw.send(ChangeDeleted)
	time.Sleep(50 * time.Millisecond)

	// Create new file with same name.
	writeFile(t, path, []byte("endofworld\n"))
	fw.send(ChangeCreated)

	n, err = wait()
	if err != nil {
		t.Fatalf("endofworld: unexpected error: %v", err)
	}
	if string(buf[:n]) != "endofworld" {
		t.Fatalf("endofworld: got %q, want %q", string(buf[:n]), "endofworld")
	}
}

// ---------------------------------------------------------------------------
// Phase 4d: MaxLineSize chunking and trailing-data fix
// ---------------------------------------------------------------------------

// TestTail_TrailingData_NoFollow verifies that a file without a trailing
// newline returns all lines including the last one (the stateEOF fix).
func TestTail_TrailingData_NoFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// No trailing newline on last line.
	writeFile(t, path, []byte("hello\nworld"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    false,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Read "hello".
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if err != nil {
		t.Fatalf("hello: unexpected error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("hello: got %q, want %q", string(buf[:n]), "hello")
	}

	// Read "world" — the trailing data without newline.
	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("world: unexpected error: %v", err)
	}
	if string(buf[:n]) != "world" {
		t.Fatalf("world: got %q, want %q", string(buf[:n]), "world")
	}

	// EOF.
	n, err = tail.ReadLine(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got n=%d, err=%v", n, err)
	}
	if n != 0 {
		t.Fatalf("EOF: expected n=0, got %d", n)
	}
}

// TestTail_TrailingData_NoFollow_EmptyFile verifies that an empty file
// returns io.EOF immediately (no phantom empty line).
func TestTail_TrailingData_NoFollow_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte{})

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    false,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got n=%d, err=%v", n, err)
	}
	if n != 0 {
		t.Fatalf("EOF: expected n=0, got %d", n)
	}
}

// TestTail_MaxLineSize_Chunking verifies that lines exceeding MaxLineSize
// are returned in chunks with ErrMaxLineSizeExceeded.
func TestTail_MaxLineSize_Chunking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// 12 a's + newline = 13 bytes. MaxLineSize=5.
	writeFile(t, path, []byte(strings.Repeat("a", 12)+"\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:      false,
		MaxLineSize: 5,
		Delimiter:   '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Chunk 1: 5 a's + ErrMaxLineSizeExceeded (idx >= MaxLineSize).
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if !errors.Is(err, ErrMaxLineSizeExceeded) {
		t.Fatalf("chunk 1: expected ErrMaxLineSizeExceeded, got n=%d, err=%v", n, err)
	}
	if string(buf[:n]) != "aaaaa" {
		t.Fatalf("chunk 1: got %q, want %q", string(buf[:n]), "aaaaa")
	}

	// Chunk 2: 5 a's + ErrMaxLineSizeExceeded.
	n, err = tail.ReadLine(buf)
	if !errors.Is(err, ErrMaxLineSizeExceeded) {
		t.Fatalf("chunk 2: expected ErrMaxLineSizeExceeded, got n=%d, err=%v", n, err)
	}
	if string(buf[:n]) != "aaaaa" {
		t.Fatalf("chunk 2: got %q, want %q", string(buf[:n]), "aaaaa")
	}

	// Remainder: 2 a's + newline → "aa" + nil.
	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("remainder: unexpected error: %v", err)
	}
	if string(buf[:n]) != "aa" {
		t.Fatalf("remainder: got %q, want %q", string(buf[:n]), "aa")
	}

	// EOF.
	n, err = tail.ReadLine(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got n=%d, err=%v", n, err)
	}
}

// TestTail_MaxLineSize_ShortBuffer verifies ErrShortBuffer takes priority
// when the caller's buffer is too small for one MaxLineSize chunk.
func TestTail_MaxLineSize_ShortBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	writeFile(t, path, []byte(strings.Repeat("a", 10)+"\n"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:      false,
		MaxLineSize: 5,
		Delimiter:   '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	// Buffer too small even for one MaxLineSize chunk.
	buf := make([]byte, 3)
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if !errors.Is(err, ErrShortBuffer) {
		t.Fatalf("expected ErrShortBuffer, got n=%d, err=%v", n, err)
	}
	if n != 0 {
		t.Fatalf("expected n=0, got %d", n)
	}
}

// TestTail_MaxLineSize_TrailingData verifies chunking of the last line
// when it has no trailing delimiter.
func TestTail_MaxLineSize_TrailingData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// "abcdefgh" — 8 bytes, no delimiter. MaxLineSize=3.
	writeFile(t, path, []byte("abcdefgh"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:      false,
		MaxLineSize: 3,
		Delimiter:   '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Chunk 1: "abc" + ErrMaxLineSizeExceeded (idx < 0, n >= MaxLineSize).
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)
	fw.send(ChangeCreated)

	n, err := wait()
	if !errors.Is(err, ErrMaxLineSizeExceeded) {
		t.Fatalf("chunk 1: expected ErrMaxLineSizeExceeded, got n=%d, err=%v", n, err)
	}
	if string(buf[:n]) != "abc" {
		t.Fatalf("chunk 1: got %q, want %q", string(buf[:n]), "abc")
	}

	// Chunk 2: "def" + ErrMaxLineSizeExceeded.
	n, err = tail.ReadLine(buf)
	if !errors.Is(err, ErrMaxLineSizeExceeded) {
		t.Fatalf("chunk 2: expected ErrMaxLineSizeExceeded, got n=%d, err=%v", n, err)
	}
	if string(buf[:n]) != "def" {
		t.Fatalf("chunk 2: got %q, want %q", string(buf[:n]), "def")
	}

	// Remainder: "gh" + nil (probed EOF, returned trailing data).
	n, err = tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("remainder: unexpected error: %v", err)
	}
	if string(buf[:n]) != "gh" {
		t.Fatalf("remainder: got %q, want %q", string(buf[:n]), "gh")
	}

	// EOF.
	n, err = tail.ReadLine(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got n=%d, err=%v", n, err)
	}
}


// ---------------------------------------------------------------------------
// Phase 4e: Regression tests for stateEOF event handlers with buffered data
// ---------------------------------------------------------------------------

// TestTail_ChangeDeleted_WithBufferedData verifies that when ChangeDeleted fires
// while the reader holds buffered trailing data (a partial last line without
// delimiter), the data is drained and returned before the old file handle
// is closed.
func TestTail_ChangeDeleted_WithBufferedData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// "hello\nworld" — 11 bytes, no trailing newline on "world".
	writeFile(t, path, []byte("hello\nworld"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Retry:     false, // don't retry after drain — just verify drain happens
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// Read first line.
	n, err := tail.ReadLine(buf)
	if err != nil {
		t.Fatalf("line 1: unexpected error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("line 1: got %q, want %q", string(buf[:n]), "hello")
	}

	// ReadLine again — "world" is buffered with no delimiter, blocks at stateEOF.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	// Simulate file deletion (log rotation). The old file handle closes,
	// but buffered "world" should be drained and returned first.
	fw.send(ChangeDeleted)

	n, err = wait()
	if err != nil {
		t.Fatalf("trailing data: unexpected error: %v", err)
	}
	if string(buf[:n]) != "world" {
		t.Fatalf("trailing data: got %q, want %q", string(buf[:n]), "world")
	}

	// Next ReadLine: stateNotOpen with Retry=false → error.
	n, err = tail.ReadLine(buf)
	if err == nil {
		t.Fatalf("expected error after drain+close, got n=%d, err=nil", n)
	}
	t.Logf("after drain+close: %v", err)
}

// TestTail_ChangeModified_WithBufferedData verifies that when ChangeModified
// fires while the reader holds buffered partial data (incomplete line), the
// probe Peek preserves the existing buffer and merges it with newly appended
// file content to form a complete line.
func TestTail_ChangeModified_WithBufferedData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// "partial" — 7 bytes, no delimiter.
	writeFile(t, path, []byte("partial"))

	fw := newFakeWatcher()

	cfg := Config{
		Follow:    true,
		Delimiter: '\n',
	}
	tail := newTailWithWatcher(context.Background(), path, cfg, fw)
	defer tail.Close()

	buf := make([]byte, 1024)

	// ReadLine blocks at stateEOF: "partial" buffered, no delimiter found,
	// n=7 < MaxLineSize=4096 → stateEOF → BlockUntilChange.
	wait := startReadLine(t, tail, buf)
	time.Sleep(50 * time.Millisecond)

	// Append "_done\n" to the file, making it "partial_done\n".
	appendFile(t, path, []byte("_done\n"))

	// Notify that the file was modified.
	fw.send(ChangeModified)

	// The probe Peek should force bufio.Reader to fill from the file,
	// merging old buffered "partial" with new "_done\n" → "partial_done".
	n, err := wait()
	if err != nil {
		t.Fatalf("merged line: unexpected error: %v", err)
	}
	if string(buf[:n]) != "partial_done" {
		t.Fatalf("merged line: got %q, want %q", string(buf[:n]), "partial_done")
	}
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

	t.Run("seek from middle", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")

		// File: a\nb\nc\n = 6 bytes. Seek -4 from end = position 2 = "b\nc\n".
		writeFile(t, path, []byte("a\nb\nc\n"))

		cfg := Config{
			Seek:      SeekInfo{Offset: -4, Whence: io.SeekEnd},
			Follow:    false,
			Delimiter: '\n',
		}
		tail := factory(t, context.Background(), path, cfg)
		defer tail.Close()

		buf := make([]byte, 1024)

		n, err := tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("line b: unexpected error: %v", err)
		}
		if string(buf[:n]) != "b" {
			t.Fatalf("line b: got %q, want %q", string(buf[:n]), "b")
		}

		n, err = tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("line c: unexpected error: %v", err)
		}
		if string(buf[:n]) != "c" {
			t.Fatalf("line c: got %q, want %q", string(buf[:n]), "c")
		}

		n, err = tail.ReadLine(buf)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected io.EOF, got n=%d, err=%v", n, err)
		}
	})

	t.Run("file rotation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		rotated := filepath.Join(dir, "test.log.rotated")

		writeFile(t, path, []byte("old1\nold2\n"))

		cfg := Config{
			Follow:    true,
			Retry:     true,
			Delimiter: '\n',
		}
		tail := factory(t, context.Background(), path, cfg)
		defer tail.Close()

		buf := make([]byte, 1024)

		// Read initial content.
		n, err := tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("old1: unexpected error: %v", err)
		}
		if string(buf[:n]) != "old1" {
			t.Fatalf("old1: got %q, want %q", string(buf[:n]), "old1")
		}

		n, err = tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("old2: unexpected error: %v", err)
		}
		if string(buf[:n]) != "old2" {
			t.Fatalf("old2: got %q, want %q", string(buf[:n]), "old2")
		}

		// ReadLine blocks waiting for more data.
		type result struct {
			n   int
			err error
		}
		ch := make(chan result, 1)
		go func() {
			n, err := tail.ReadLine(buf)
			ch <- result{n, err}
		}()

		// Give the watcher time to reach BlockUntilChange.
		time.Sleep(100 * time.Millisecond)

		// Rename the file (simulates log rotation).
		if err := os.Rename(path, rotated); err != nil {
			t.Fatalf("os.Rename: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		// Create a new file with the same name.
		writeFile(t, path, []byte("new1\nnew2\n"))

		r := <-ch
		if r.err != nil {
			t.Fatalf("new1: unexpected error: %v", r.err)
		}
		if string(buf[:r.n]) != "new1" {
			t.Fatalf("new1: got %q, want %q", string(buf[:r.n]), "new1")
		}

		n, err = tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("new2: unexpected error: %v", err)
		}
		if string(buf[:n]) != "new2" {
			t.Fatalf("new2: got %q, want %q", string(buf[:n]), "new2")
		}
	})

	t.Run("trailing data, no follow", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")

		// No trailing newline on last line.
		writeFile(t, path, []byte("hello\nworld"))

		cfg := Config{
			Follow:    false,
			Delimiter: '\n',
		}
		tail := factory(t, context.Background(), path, cfg)
		defer tail.Close()

		buf := make([]byte, 1024)

		n, err := tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("hello: unexpected error: %v", err)
		}
		if string(buf[:n]) != "hello" {
			t.Fatalf("hello: got %q, want %q", string(buf[:n]), "hello")
		}

		n, err = tail.ReadLine(buf)
		if err != nil {
			t.Fatalf("world: unexpected error: %v", err)
		}
		if string(buf[:n]) != "world" {
			t.Fatalf("world: got %q, want %q", string(buf[:n]), "world")
		}

		n, err = tail.ReadLine(buf)
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected io.EOF, got n=%d, err=%v", n, err)
		}
		if n != 0 {
			t.Fatalf("EOF: expected n=0, got %d", n)
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
