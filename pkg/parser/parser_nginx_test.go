// LLM usage: generated with deepseek-v4-pro and manually modified.
package parser

import (
	"net/netip"
	"testing"
	"time"

	"github.com/HT4w5/webjail/pkg/request"
)

func TestNginxParser_Parse(t *testing.T) {
	np := NewNginxParser()

	tests := []struct {
		name    string
		line    string
		want    request.Request
		wantErr bool
	}{
		{
			name: "root path 200",
			line: `<190>Jul 23 14:42:57 168e91ae9a94 nginx: {"body_bytes_sent":17720,"https":"","msec":"1784788977.093","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.001","status":200,"uri":"/","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: request.Request{
				Method:     "GET",
				Path:       "/",
				UserAgent:  "Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				HTTPS:      false,
				ClientAddr: netip.MustParseAddr("192.168.114.1"),
				Status:     200,
				BodySent:   17720,
				Duration:   1 * time.Millisecond,
				Time:       time.Unix(1784788977, 93000000).UTC(),
			},
		},
		{
			name: "404 not found",
			line: `<190>Jul 23 14:42:57 168e91ae9a94 nginx: {"body_bytes_sent":68,"https":"","msec":"1784788977.107","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.001","status":404,"uri":"/js/smartquotes.js","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: request.Request{
				Method:     "GET",
				Path:       "/js/smartquotes.js",
				UserAgent:  "Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				HTTPS:      false,
				ClientAddr: netip.MustParseAddr("192.168.114.1"),
				Status:     404,
				BodySent:   68,
				Duration:   1 * time.Millisecond,
				Time:       time.Unix(1784788977, 107000000).UTC(),
			},
		},
		{
			name: "large body_bytes_sent (bootstrap.min.css)",
			line: `<190>Jul 23 14:42:57 168e91ae9a94 nginx: {"body_bytes_sent":122262,"https":"","msec":"1784788977.111","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.001","status":200,"uri":"/css/bootstrap.min.css","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: request.Request{
				Method:     "GET",
				Path:       "/css/bootstrap.min.css",
				UserAgent:  "Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				HTTPS:      false,
				ClientAddr: netip.MustParseAddr("192.168.114.1"),
				Status:     200,
				BodySent:   122262,
				Duration:   1 * time.Millisecond,
				Time:       time.Unix(1784788977, 111000000).UTC(),
			},
		},
		{
			name: "PDF file with longer request_time",
			line: `<190>Jul 23 14:43:15 168e91ae9a94 nginx: {"body_bytes_sent":6520587,"https":"","msec":"1784788995.834","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.021","status":200,"uri":"/handouts/week-1-logistics.pdf","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: request.Request{
				Method:     "GET",
				Path:       "/handouts/week-1-logistics.pdf",
				UserAgent:  "Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				HTTPS:      false,
				ClientAddr: netip.MustParseAddr("192.168.114.1"),
				Status:     200,
				BodySent:   6520587,
				Duration:   21 * time.Millisecond,
				Time:       time.Unix(1784788995, 834000000).UTC(),
			},
		},
		{
			name: "lab_faq.html",
			line: `<190>Jul 23 14:43:09 168e91ae9a94 nginx: {"body_bytes_sent":14877,"https":"","msec":"1784788989.841","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.002","status":200,"uri":"/lab_faq.html","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: request.Request{
				Method:     "GET",
				Path:       "/lab_faq.html",
				UserAgent:  "Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				HTTPS:      false,
				ClientAddr: netip.MustParseAddr("192.168.114.1"),
				Status:     200,
				BodySent:   14877,
				Duration:   2 * time.Millisecond,
				Time:       time.Unix(1784788989, 841000000).UTC(),
			},
		},
		{
			name: "SVG image (large body)",
			line: `<190>Jul 23 14:42:57 168e91ae9a94 nginx: {"body_bytes_sent":269878,"https":"","msec":"1784788977.117","remote_addr":"192.168.114.1","request_method":"GET","request_time":"0.003","status":200,"uri":"/images/keithw-email.svg","http_user_agent":"Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0"}`,
			want: request.Request{
				Method:     "GET",
				Path:       "/images/keithw-email.svg",
				UserAgent:  "Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
				HTTPS:      false,
				ClientAddr: netip.MustParseAddr("192.168.114.1"),
				Status:     200,
				BodySent:   269878,
				Duration:   3 * time.Millisecond,
				Time:       time.Unix(1784788977, 117000000).UTC(),
			},
		},
		{
			name:    "no JSON in line",
			line:    `<190>Jul 23 14:42:57 168e91ae9a94 nginx: no json here`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req request.Request
			err := np.Parse([]byte(tt.line), &req)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if req.Method != tt.want.Method {
				t.Errorf("Method = %q, want %q", req.Method, tt.want.Method)
			}
			if req.Path != tt.want.Path {
				t.Errorf("Path = %q, want %q", req.Path, tt.want.Path)
			}
			if req.UserAgent != tt.want.UserAgent {
				t.Errorf("UserAgent = %q, want %q", req.UserAgent, tt.want.UserAgent)
			}
			if req.HTTPS != tt.want.HTTPS {
				t.Errorf("HTTPS = %v, want %v", req.HTTPS, tt.want.HTTPS)
			}
			if req.ClientAddr != tt.want.ClientAddr {
				t.Errorf("ClientAddr = %v, want %v", req.ClientAddr, tt.want.ClientAddr)
			}
			if req.Status != tt.want.Status {
				t.Errorf("Status = %d, want %d", req.Status, tt.want.Status)
			}
			if req.BodySent != tt.want.BodySent {
				t.Errorf("BodySent = %d, want %d", req.BodySent, tt.want.BodySent)
			}
			if req.Duration != tt.want.Duration {
				t.Errorf("Duration = %v, want %v", req.Duration, tt.want.Duration)
			}
			if !req.Time.Equal(tt.want.Time) {
				t.Errorf("Time = %v, want %v", req.Time, tt.want.Time)
			}
		})
	}
}
