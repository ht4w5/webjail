// LLM usage: the fakelog utility is generated with deepseek-v4-pro and manually modified.
// fakelog generates random text lines at a configurable interval,
// writing to stdout, stderr, or a file.
//
// Usage:
//
//	fakelog [flags]
//
// Flags:
//
//	-i, --interval   per-line delay (Go duration, default "1s")
//	-o, --output     destination: "stdout", "stderr", or file path (default "stdout")
//	-n, --count      max lines to emit, 0 = unlimited (default 0)
//	-d, --duration   max run duration (Go duration, 0 = unlimited, default 0)
//	-l, --length     characters per random line (default 80)
//	    --charset    character set: alphanumeric, alpha, numeric, hex, ascii (default "alphanumeric")
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/pflag"
)

func main() {
	interval := pflag.DurationP("interval", "i", time.Second, "per-line delay")
	output := pflag.StringP("output", "o", "stdout", "destination: stdout, stderr, or file path")
	count := pflag.Int64P("count", "n", 0, "max lines (0 = unlimited)")
	duration := pflag.DurationP("duration", "d", 0, "max run time (0 = unlimited)")
	length := pflag.IntP("length", "l", 80, "characters per random line")
	charset := pflag.String("charset", "alphanumeric", "character set: alphanumeric, alpha, numeric, hex, ascii")

	pflag.Parse()

	// Validate.
	if *interval <= 0 {
		fatalf("interval must be positive, got %v", *interval)
	}
	if *length <= 0 {
		fatalf("length must be positive, got %d", *length)
	}
	if *count < 0 {
		fatalf("count must be >= 0, got %d", *count)
	}

	chars, ok := charsets[*charset]
	if !ok {
		fatalf("unknown charset %q (valid: alphanumeric, alpha, numeric, hex, ascii)", *charset)
	}

	w, closeW, err := openOutput(*output)
	if err != nil {
		fatalf("%v", err)
	}
	defer closeW()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *duration > 0 {
		var dCancel context.CancelFunc
		ctx, dCancel = context.WithTimeout(ctx, *duration)
		defer dCancel()
	}

	if err := run(ctx, w, *interval, *count, *length, chars); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		fatalf("%v", err)
	}
}

// run emits random lines to w on each tick of a ticker configured to interval.
// It stops when ctx is done or count lines have been written (count=0 means unlimited).
func run(ctx context.Context, w io.Writer, interval time.Duration, count int64, length int, chars []byte) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var written int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			line := generateLine(length, chars)
			if _, err := fmt.Fprintln(w, line); err != nil {
				return fmt.Errorf("write: %w", err)
			}
			written++
			if count > 0 && written >= count {
				return nil
			}
		}
	}
}

// generateLine builds a random string of n bytes drawn from chars.
func generateLine(n int, chars []byte) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.IntN(len(chars))]
	}
	return string(b)
}

// openOutput returns an io.Writer and a close function for the given destination.
// "stdout" and "stderr" map to os.Stdout / os.Stderr; anything else is treated
// as a file path and opened with os.Create (truncating any existing file).
func openOutput(dest string) (io.WriteCloser, func(), error) {
	noop := func() {}
	switch dest {
	case "stdout":
		return nopWriteCloser{os.Stdout}, noop, nil
	case "stderr":
		return nopWriteCloser{os.Stderr}, noop, nil
	default:
		f, err := os.Create(dest)
		if err != nil {
			return nil, nil, fmt.Errorf("open output file: %w", err)
		}
		return f, func() { f.Close() }, nil
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fakelog: "+format+"\n", args...)
	os.Exit(1)
}

var charsets = map[string][]byte{
	"alphanumeric": []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"),
	"alpha":        []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"),
	"numeric":      []byte("0123456789"),
	"hex":          []byte("0123456789abcdef"),
	"ascii":        asciiPrintable(),
}

// asciiPrintable returns all printable ASCII characters (0x21–0x7e).
func asciiPrintable() []byte {
	b := make([]byte, 0, 95)
	for c := byte(0x21); c <= 0x7e; c++ {
		b = append(b, c)
	}
	return b
}
