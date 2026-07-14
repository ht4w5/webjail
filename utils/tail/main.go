// LLM usage: the tail utility is generated with deepseek-v4-pro and manually modified.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/HT4w5/webjail/pkg/tail"
)

func main() {
	follow := pflag.BoolP("follow", "f", false, "output appended data as the file grows")
	retry := pflag.BoolP("retry", "F", false, "keep trying to open a file if it is inaccessible")
	poll := pflag.Bool("poll", false, "use poll watcher instead of fsnotify")
	noSeek := pflag.Bool("no-seek", false, "disable seeking (for named pipes)")
	lines := pflag.StringP("lines", "n", "10", "output the last N lines, or +N to start from line N")
	bytes := pflag.Int64P("bytes", "c", 0, "output the last N bytes")
	delimiter := pflag.String("delimiter", "\\n", "line delimiter (\\n, \\0, \\t, \\r, or a single byte)")

	pflag.Parse()

	// -F implies -f: Retry is ignored when Follow is false.
	if *retry {
		*follow = true
	}

	delim, err := parseDelimiter(*delimiter)
	if err != nil {
		fatalf("invalid delimiter: %v", err)
	}

	args := pflag.Args()
	if len(args) == 0 {
		pflag.Usage()
		os.Exit(1)
	}
	filename := args[0]

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, filename, *follow, *retry, *poll, *noSeek, delim, *lines, *bytes); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "tail: "+format+"\n", args...)
	os.Exit(1)
}

func parseDelimiter(s string) (byte, error) {
	switch s {
	case "\\n":
		return '\n', nil
	case "\\0":
		return 0, nil
	case "\\t":
		return '\t', nil
	case "\\r":
		return '\r', nil
	default:
		if len(s) == 1 {
			return s[0], nil
		}
		return 0, fmt.Errorf("%q must be a single byte or escape sequence", s)
	}
}

func run(ctx context.Context, filename string, follow, retry, poll, noSeek bool, delimiter byte, linesFlag string, bytesFlag int64) error {
	fromStart := false
	lineCount := 0

	// Determine line-count mode (default: -n 10).
	{
		ls := linesFlag
		if strings.HasPrefix(ls, "+") {
			fromStart = true
			ls = ls[1:]
		}
		n, err := strconv.Atoi(ls)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid number of lines: %q", linesFlag)
		}
		lineCount = n
	}

	// Phase 1: read existing content (Follow=false).
	// All modes share this two-phase structure because the tail package
	// only performs eager open when Follow=false.
	var phase1Cfg tail.Config
	var num int
	var fs bool

	switch {
	case bytesFlag > 0:
		phase1Cfg = buildConfig(false, false, poll, false, delimiter)
		phase1Cfg.Seek = tail.SeekInfo{Offset: -bytesFlag, Whence: io.SeekEnd}
		num, fs = 0, false
	case fromStart && lineCount > 0:
		phase1Cfg = buildConfig(false, false, poll, false, delimiter)
		phase1Cfg.Seek = tail.SeekInfo{Offset: 0, Whence: io.SeekStart}
		num, fs = lineCount, true
	default:
		phase1Cfg = buildConfig(false, false, poll, false, delimiter)
		phase1Cfg.Seek = tail.SeekInfo{Offset: 0, Whence: io.SeekStart}
		num = lineCount
		if num == 0 {
			num = 10
		}
		fs = false
	}

	if err := tailAndPrint(ctx, filename, phase1Cfg, num, fs, delimiter); err != nil {
		return err
	}
	if !follow {
		return nil
	}

	// Phase 2: follow new data from end.
	phase2Cfg := buildConfig(true, retry, poll, noSeek, delimiter)
	phase2Cfg.Seek = tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}
	return tailAndPrint(ctx, filename, phase2Cfg, 0, false, delimiter)
}

func buildConfig(follow, retry, poll, noSeek bool, delimiter byte) tail.Config {
	return tail.Config{
		Follow:    follow,
		Retry:     retry,
		Poll:      poll,
		NoSeek:    noSeek,
		Delimiter: delimiter,
	}
}

// tailAndPrint creates a Tail and prints lines to stdout.
//
// When fromStart is true, skips the first numLines-1 lines then prints the
// rest directly. When fromStart is false and numLines > 0, buffers all lines
// in a ring buffer and flushes the last numLines on EOF. When numLines is 0,
// prints all lines directly (used for -c mode and follow-only mode).
func tailAndPrint(ctx context.Context, filename string, cfg tail.Config, numLines int, fromStart bool, delimiter byte) error {
	t, err := tail.New(ctx, filename, cfg)
	if err != nil {
		return err
	}
	defer t.Close()

	buf := make([]byte, 4096)
	rb := newRingBuffer(numLines)
	skip := 0
	useBuffer := numLines > 0 && !fromStart

	if fromStart && numLines > 0 {
		skip = numLines - 1
	}

	for {
		n, err := t.ReadLine(buf)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if errors.Is(err, tail.ErrBufferTooShort) {
				buf = make([]byte, len(buf)*2)
				continue
			}
			return err
		}

		if skip > 0 {
			skip--
			continue
		}

		if useBuffer {
			rb.Add(string(buf[:n]))
		} else {
			os.Stdout.Write(buf[:n])
			os.Stdout.Write([]byte{delimiter})
		}
	}

	// Flush ring buffer after EOF.
	if useBuffer {
		for _, line := range rb.Lines() {
			os.Stdout.WriteString(line)
			os.Stdout.Write([]byte{delimiter})
		}
	}

	return nil
}

// ringBuffer holds the last N strings in a circular buffer.
type ringBuffer struct {
	buf  []string
	size int
	pos  int
	full bool
}

func newRingBuffer(size int) *ringBuffer {
	if size <= 0 {
		size = 0
	}
	return &ringBuffer{buf: make([]string, size), size: size}
}

func (rb *ringBuffer) Add(line string) {
	if rb.size == 0 {
		return
	}
	rb.buf[rb.pos] = line
	rb.pos = (rb.pos + 1) % rb.size
	if rb.pos == 0 {
		rb.full = true
	}
}

// Lines returns the buffered lines in insertion order.
func (rb *ringBuffer) Lines() []string {
	if rb.size == 0 {
		return nil
	}
	if !rb.full {
		return rb.buf[:rb.pos]
	}
	result := make([]string, rb.size)
	n := copy(result, rb.buf[rb.pos:])
	copy(result[n:], rb.buf[:rb.pos])
	return result
}
