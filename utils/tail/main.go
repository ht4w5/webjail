// LLM usage: the tail utility is generated with deepseek-v4-pro.
// tail is a Unix tail(1) clone that outputs the last part of files.
//
// Usage:
//
//	tail [flags] <file>
//
// Flags:
//
//	-n, --lines      output the last NUM lines (default 10)
//	                  use -n 0 with -f to only output new data
//	-f, --follow     output appended data as the file grows
//	    --retry      keep trying to open a file if it is inaccessible (requires -f)
//	    --poll       use polling instead of fsnotify for file change detection
//	    --no-seek    disable seeking; useful for named pipes (FIFOs)
//	-d, --delimiter  line delimiter byte (default "\n")
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/HT4w5/webjail/pkg/tail"
)

const (
	maxLineSize    = 1 << 20 // 1 MiB initial ReadLine buffer
	maxLineSizeCap = 1 << 26 // 64 MiB upper bound for ReadLine buffer
)

func main() {
	lines := pflag.IntP("lines", "n", 10, "output the last NUM lines (0 with -f for new data only)")
	follow := pflag.BoolP("follow", "f", false, "output appended data as the file grows")
	retry := pflag.Bool("retry", false, "keep trying to open a file if it is inaccessible")
	poll := pflag.Bool("poll", false, "use polling instead of fsnotify for file change detection")
	noSeek := pflag.Bool("no-seek", false, "disable seeking (for named pipes)")
	delimiter := pflag.StringP("delimiter", "d", "\n", "line delimiter byte")

	pflag.Parse()

	// Validation.
	if *lines < 0 {
		fatalf("lines must be >= 0, got %d", *lines)
	}
	if *retry && !*follow {
		// Error message matches tail.New's validation: "Retry set without Follow"
		fatalf("--retry requires --follow (-f)")
	}
	if len(*delimiter) == 0 {
		fatalf("delimiter must not be empty")
	}

	args := pflag.Args()
	if len(args) != 1 {
		fatalf("expected exactly one file argument, got %d", len(args))
	}
	filename := args[0]

	cfg := tail.Config{
		Follow:    *follow,
		Retry:     *retry,
		Poll:      *poll,
		NoSeek:    *noSeek,
		Delimiter: (*delimiter)[0],
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, filename, cfg, *lines); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		fatalf("%v", err)
	}
}

// run dispatches to the appropriate strategy based on flags.
func run(ctx context.Context, filename string, cfg tail.Config, numLines int) error {
	switch {
	case cfg.Follow && numLines > 0:
		return followWithHead(ctx, filename, cfg, numLines)
	case cfg.Follow && numLines == 0:
		return followOnly(ctx, filename, cfg)
	default:
		return staticTail(ctx, filename, cfg, numLines)
	}
}

// staticTail reads the file from the beginning, keeps the last numLines,
// prints them, and exits.
func staticTail(ctx context.Context, filename string, cfg tail.Config, numLines int) error {
	cfg.Follow = false
	cfg.Seek = tail.SeekInfo{Offset: 0, Whence: io.SeekStart}

	t, err := tail.New(ctx, filename, cfg)
	if err != nil {
		return fmt.Errorf("tail: %w", err)
	}
	defer t.Close()

	rb := newRingBuffer(numLines)
	buf := make([]byte, maxLineSize)

	for {
		n, err := t.ReadLine(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if errors.Is(err, tail.ErrShortBuffer) {
				buf = grow(buf)
				continue
			}
			return fmt.Errorf("read: %w", err)
		}
		rb.push(buf[:n])
	}

	rb.flush(os.Stdout)
	return nil
}

// followOnly seeks to the end of the file and prints new lines as they arrive.
func followOnly(ctx context.Context, filename string, cfg tail.Config) error {
	cfg.Follow = true
	cfg.Seek = tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}

	t, err := tail.New(ctx, filename, cfg)
	if err != nil {
		return fmt.Errorf("tail: %w", err)
	}
	defer t.Close()

	return followLoop(ctx, t)
}

// followWithHead first reads all existing lines, prints the last numLines,
// then continues following from the end of the file.
//
// A two-pass approach is used because the tail package with Follow=true
// blocks at EOF, making it impossible to distinguish "finished reading
// existing data" from "waiting for new data" within a single Tail instance.
// If the file does not exist yet (e.g. with --retry), phase 1 is skipped
// and we proceed directly to follow-only mode.
func followWithHead(ctx context.Context, filename string, cfg tail.Config, numLines int) error {
	// Phase 1: read all existing lines, print the last numLines.
	// Skip this phase if the file is not accessible (e.g. --retry mode
	// where the file has not been created yet).
	if fi, err := os.Stat(filename); err == nil && fi.Mode().IsRegular() {
		staticCfg := cfg
		staticCfg.Follow = false
		staticCfg.Retry = false // Retry requires Follow; static pass is non-following.
		staticCfg.Seek = tail.SeekInfo{Offset: 0, Whence: io.SeekStart}

		t, err := tail.New(ctx, filename, staticCfg)
		if err != nil {
			return fmt.Errorf("tail: %w", err)
		}

		rb := newRingBuffer(numLines)
		buf := make([]byte, maxLineSize)

		for {
			n, err := t.ReadLine(buf)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				if errors.Is(err, tail.ErrShortBuffer) {
					buf = grow(buf)
					continue
				}
				t.Close()
				return fmt.Errorf("read: %w", err)
			}
			rb.push(buf[:n])
		}
		t.Close()

		rb.flush(os.Stdout)
	}

	// Phase 2: follow from the current end of file.
	cfg.Follow = true
	cfg.Seek = tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}

	t, err := tail.New(ctx, filename, cfg)
	if err != nil {
		return fmt.Errorf("tail: %w", err)
	}
	defer t.Close()

	return followLoop(ctx, t)
}

// followLoop reads and prints lines from t until ctx is done or a fatal
// error occurs.
func followLoop(ctx context.Context, t *tail.Tail) error {
	buf := make([]byte, maxLineSize)

	for {
		n, err := t.ReadLine(buf)
		if err != nil {
			if errors.Is(err, context.Canceled) ||
				errors.Is(err, context.DeadlineExceeded) ||
				errors.Is(err, tail.ErrClosed) {
				return nil
			}
			if errors.Is(err, tail.ErrShortBuffer) {
				buf = grow(buf)
				continue
			}
			return fmt.Errorf("read: %w", err)
		}

		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if _, err := fmt.Println(string(buf[:n])); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}
}

// ringBuffer holds at most cap lines using modulus indexing.
type ringBuffer struct {
	lines [][]byte
	idx   int // next write position mod len(lines)
	total int // total lines pushed
}

func newRingBuffer(cap int) *ringBuffer {
	if cap <= 0 {
		cap = 1
	}
	return &ringBuffer{lines: make([][]byte, cap)}
}

// push stores a deep copy of line in the ring buffer.
func (rb *ringBuffer) push(line []byte) {
	cp := make([]byte, len(line))
	copy(cp, line)
	rb.lines[rb.idx%len(rb.lines)] = cp
	rb.idx++
	rb.total++
}

// flush writes the last min(cap, total) lines to w, one per line.
func (rb *ringBuffer) flush(w io.Writer) {
	n := len(rb.lines)
	if rb.total < n {
		n = rb.total
	}
	start := rb.total - n
	for i := start; i < rb.total; i++ {
		fmt.Fprintln(w, string(rb.lines[i%len(rb.lines)]))
	}
}

// grow doubles the buffer size, capped at maxLineSizeCap.
// If the cap is already reached, it returns the original slice.
func grow(buf []byte) []byte {
	if len(buf) >= maxLineSizeCap {
		return buf
	}
	return make([]byte, len(buf)*2)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "tail: "+format+"\n", args...)
	os.Exit(1)
}
