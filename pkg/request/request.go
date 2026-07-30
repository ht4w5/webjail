package request

import (
	"net/netip"
	"time"

	"github.com/HT4w5/webjail/pkg/filesize"
	"github.com/rs/zerolog"
	"github.com/valyala/bytebufferpool"
)

// Request represents a web request
type Request struct {
	ClientAddr netip.Addr
	Time       time.Time
	Method     string
	Path       *bytebufferpool.ByteBuffer
	UserAgent  *bytebufferpool.ByteBuffer
	Status     int
	BodySent   int64
	Filesize   int64
	Duration   time.Duration
	HTTPS      bool
}

func New() *Request {
	return &Request{
		Filesize: filesize.NoInfo,
	}
}

func (req *Request) reset() {
	*req = Request{}
}

func (req *Request) MarshalZerologObject(e *zerolog.Event) {
	e.Stringer("client_addr", req.ClientAddr).
		Time("time", req.Time).
		Str("method", req.Method).
		Stringer("path", req.Path).
		Stringer("user_agent", req.UserAgent).
		Int("status", req.Status).
		Int64("body_sent", req.BodySent).
		Dur("duration", req.Duration).
		Bool("https", req.HTTPS)
}
