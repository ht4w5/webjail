// LLM usage: generated with deepseek-v4-pro and manually modified.
package parser

import (
	"net/netip"
	"testing"
	"time"

	"github.com/HT4w5/webjail/pkg/request"
	"github.com/valyala/bytebufferpool"
)

func TestNginxParser_Parse(t *testing.T) {
	np := NewNginxParser()

	tests := []struct {
		name    string
		line    string
		want    *request.Request
		wantErr bool
	}{
		{
			name: "root path 200",
			line: `<190>Jul 23 14:42:57 168e91ae9a94 nginx: {"body_bytes_sent":17720,"https":"","msec":"1784788977.093","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.001","status":200,"uri":"/","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: wantRequest(
				"GET", "/",
				"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				false,
				"192.168.114.1",
				200, 17720,
				1*time.Millisecond,
				time.Unix(1784788977, 93000000).UTC(),
			),
		},
		{
			name: "404 not found",
			line: `<190>Jul 23 14:42:57 168e91ae9a94 nginx: {"body_bytes_sent":68,"https":"","msec":"1784788977.107","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.001","status":404,"uri":"/js/smartquotes.js","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: wantRequest(
				"GET", "/js/smartquotes.js",
				"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				false,
				"192.168.114.1",
				404, 68,
				1*time.Millisecond,
				time.Unix(1784788977, 107000000).UTC(),
			),
		},
		{
			name: "large body_bytes_sent (bootstrap.min.css)",
			line: `<190>Jul 23 14:42:57 168e91ae9a94 nginx: {"body_bytes_sent":122262,"https":"","msec":"1784788977.111","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.001","status":200,"uri":"/css/bootstrap.min.css","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: wantRequest(
				"GET", "/css/bootstrap.min.css",
				"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				false,
				"192.168.114.1",
				200, 122262,
				1*time.Millisecond,
				time.Unix(1784788977, 111000000).UTC(),
			),
		},
		{
			name: "PDF file with longer request_time",
			line: `<190>Jul 23 14:43:15 168e91ae9a94 nginx: {"body_bytes_sent":6520587,"https":"","msec":"1784788995.834","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.021","status":200,"uri":"/handouts/week-1-logistics.pdf","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: wantRequest(
				"GET", "/handouts/week-1-logistics.pdf",
				"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				false,
				"192.168.114.1",
				200, 6520587,
				21*time.Millisecond,
				time.Unix(1784788995, 834000000).UTC(),
			),
		},
		{
			name: "lab_faq.html",
			line: `<190>Jul 23 14:43:09 168e91ae9a94 nginx: {"body_bytes_sent":14877,"https":"","msec":"1784788989.841","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.002","status":200,"uri":"/lab_faq.html","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: wantRequest(
				"GET", "/lab_faq.html",
				"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				false,
				"192.168.114.1",
				200, 14877,
				2*time.Millisecond,
				time.Unix(1784788989, 841000000).UTC(),
			),
		},
		{
			name: "SVG image (large body)",
			line: `<190>Jul 23 14:42:57 168e91ae9a94 nginx: {"body_bytes_sent":269878,"https":"","msec":"1784788977.117","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.003","status":200,"uri":"/images/keithw-email.svg","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: wantRequest(
				"GET", "/images/keithw-email.svg",
				"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				false,
				"192.168.114.1",
				200, 269878,
				3*time.Millisecond,
				time.Unix(1784788977, 117000000).UTC(),
			),
		},
		{
			name:    "no JSON in line",
			line:    `<190>Jul 23 14:42:57 168e91ae9a94 nginx: no json here`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := request.Get()
			err := np.Parse([]byte(tt.line), got)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !requestEqual(got, tt.want) {
				t.Errorf("requests not equal:\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

// requestEqual reports whether two requests are equal in all observable fields.
func requestEqual(a, b *request.Request) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ClientAddr == b.ClientAddr &&
		a.Time.Equal(b.Time) &&
		a.Method == b.Method &&
		bbEqual(a.Path, b.Path) &&
		bbEqual(a.UserAgent, b.UserAgent) &&
		a.Status == b.Status &&
		a.BodySent == b.BodySent &&
		a.Duration == b.Duration &&
		a.HTTPS == b.HTTPS
}

func bbEqual(a, b *bytebufferpool.ByteBuffer) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.String() == b.String()
}

// wantRequest builds a *request.Request for test expectations.
func wantRequest(method, path, ua string, https bool, addr string, status int, bodySent int64, dur time.Duration, t time.Time) *request.Request {
	req := request.Get()
	req.Path.Reset()
	req.UserAgent.Reset()
	req.Method = method
	req.Path.WriteString(path)
	req.UserAgent.WriteString(ua)
	req.HTTPS = https
	req.ClientAddr = netip.MustParseAddr(addr)
	req.Status = status
	req.BodySent = bodySent
	req.Duration = dur
	req.Time = t
	return req
}
