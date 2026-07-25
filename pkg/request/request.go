package request

import (
	"net/netip"
	"time"

	"github.com/rs/zerolog"
)

// Request represents a web request
type Request struct {
	ClientAddr netip.Addr
	Time       time.Time
	Method     string
	Path       string
	UserAgent  string
	b          []byte
	Status     int
	BodySent   int64
	Duration   time.Duration
	HTTPS      bool
}

func New(bufferSize int) *Request {
	return &Request{
		b: make([]byte, bufferSize),
	}
}

func (req *Request) Bytes() []byte {
	return req.b
}

func (req *Request) MarshalZerologObject(e *zerolog.Event) {
	e.Stringer("client_addr", req.ClientAddr).
		Time("time", req.Time).
		Str("method", req.Method).
		Str("path", req.Path).
		Str("user_agent", req.UserAgent).
		Int("status", req.Status).
		Int64("body_sent", req.BodySent).
		Dur("duration", req.Duration).
		Bool("https", req.HTTPS)
}
