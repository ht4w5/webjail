// LLM usage: the udplisten utility is generated with deepseek-v4-pro.
// udplisten listens on a UDP address:port and writes incoming packets
// to stdout, stderr, or a file.
//
// Usage:
//
//	udplisten [flags]
//
// Flags:
//
//	-a, --address     UDP address:port to listen on (required)
//	-o, --output      destination: "stdout", "stderr", or file path (default "stdout")
//	-n, --no-newline  do not append a newline after each packet
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/pflag"
)

// maxDatagram is the maximum UDP datagram size (65535 bytes, the largest
// value that fits in the 16-bit length field of the UDP header).
const maxDatagram = 65535

func main() {
	address := pflag.StringP("address", "a", "", "UDP address:port to listen on (required)")
	output := pflag.StringP("output", "o", "stdout", "destination: stdout, stderr, or file path")
	noNewline := pflag.BoolP("no-newline", "n", false, "do not append newline after each packet")

	pflag.Parse()

	// Validate.
	if *address == "" {
		fatalf("--address (-a) is required")
	}

	w, closeW, err := openOutput(*output)
	if err != nil {
		fatalf("%v", err)
	}
	defer closeW()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	addNewline := !*noNewline

	if err := run(ctx, w, *address, addNewline); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		fatalf("%v", err)
	}
}

// run listens on addr for UDP packets and writes each one to w.
// If addNewline is true, a '\n' is appended after each packet body.
func run(ctx context.Context, w io.Writer, addr string, addNewline bool) error {
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve address %q: %w", addr, err)
	}

	conn, err := net.ListenUDP("udp", ua)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", addr, err)
	}
	defer conn.Close()

	buf := make([]byte, maxDatagram)

	// Watchdog goroutine: close the connection when ctx is done, so that
	// the blocking ReadFromUDP call below unblocks and returns an error.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			// On Linux, a closed UDP listener wakes ReadFromUDP with
			// a "use of closed network connection" error. Treat this
			// as a graceful shutdown.
			var netErr *net.OpError
			if errors.As(err, &netErr) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}
			return fmt.Errorf("read udp: %w", err)
		}

		if _, err := w.Write(buf[:n]); err != nil {
			return fmt.Errorf("write output: %w", err)
		}

		if addNewline {
			if _, err := w.Write([]byte{'\n'}); err != nil {
				return fmt.Errorf("write newline: %w", err)
			}
		}
	}
}

// openOutput returns an io.WriteCloser and a close function for the given
// destination. "stdout" and "stderr" map to os.Stdout / os.Stderr; anything
// else is treated as a file path and opened in append mode.
func openOutput(dest string) (io.WriteCloser, func(), error) {
	noop := func() {}
	switch dest {
	case "stdout":
		return nopWriteCloser{os.Stdout}, noop, nil
	case "stderr":
		return nopWriteCloser{os.Stderr}, noop, nil
	default:
		f, err := os.OpenFile(dest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
	fmt.Fprintf(os.Stderr, "udplisten: "+format+"\n", args...)
	os.Exit(1)
}
