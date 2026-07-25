package parser

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/HT4w5/webjail/pkg/request"
	"github.com/bytedance/sonic"
)

var json = sonic.Config{
	NoValidateJSONSkip: true,
	CaseSensitive:      true,
}.Froze()

var _ Parser = (*NginxParser)(nil)

type NginxParser struct {
}

type nginxJSONObject struct {
	RequestMethod string `json:"request_method"`
	URI           string `json:"uri"`
	HTTPUserAgent string `json:"http_user_agent"`
	HTTPS         string `json:"https"`
	Msec          string `json:"msec"`
	RemoteAddr    string `json:"remote_addr"`
	RequestTime   string `json:"request_time"`
	BodyBytesSent int64  `json:"body_bytes_sent"`
	Status        int    `json:"status"`
}

func NewNginxParser() *NginxParser {
	return &NginxParser{}
}

func (np *NginxParser) Parse(b []byte, req *request.Request) error {
	// Slice out valid JSON
	var err error
	b, err = extractFirstJSON(b)
	if err != nil {
		return err
	}

	var obj nginxJSONObject

	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("unmarshal json: %w", err)
	}

	req.Method = obj.RequestMethod
	req.Path = obj.URI
	req.UserAgent = obj.HTTPUserAgent

	if obj.HTTPS == "on" {
		req.HTTPS = true
	} else {
		req.HTTPS = false
	}

	time, err := parseNginxTime(obj.Msec)
	if err != nil {
		return fmt.Errorf("parse msec: %w", err)
	}
	req.Time = time

	caddr, err := netip.ParseAddr(obj.RemoteAddr)
	if err != nil {
		return fmt.Errorf("parse remote_addr: %w", err)
	}
	req.ClientAddr = caddr

	duration, err := parseNginxDuration(obj.RequestTime)
	if err != nil {
		return fmt.Errorf("parse request_time: %w", err)
	}
	req.Duration = duration

	req.BodySent = obj.BodyBytesSent
	req.Status = obj.Status

	return err
}

// Helpers

var pow10 = [10]int64{1, 10, 100, 1000, 10000, 100000, 1000000, 10000000, 100000000, 1000000000}

func parseNginxDuration(s string) (time.Duration, error) {
	dotIdx := strings.IndexByte(s, '.')
	if dotIdx < 0 {
		sec, err := strconv.ParseInt(s, 10, 64)
		return time.Duration(sec * 1e9), err
	}

	sec, err := strconv.ParseInt(s[:dotIdx], 10, 64)
	if err != nil {
		return 0, err
	}

	frac := s[dotIdx+1:]
	fracN, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, err
	}

	nsec := (fracN * 1e9) / pow10[len(frac)]

	return time.Duration(sec*1e9 + nsec), nil
}

func parseNginxTime(s string) (time.Time, error) {
	dotIdx := strings.IndexByte(s, '.')
	if dotIdx < 0 {
		sec, err := strconv.ParseInt(s, 10, 64)
		return time.Unix(sec, 0).UTC(), err
	}

	sec, err := strconv.ParseInt(s[:dotIdx], 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	frac := s[dotIdx+1:]
	fracN, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	nsec := (fracN * 1e9) / pow10[len(frac)]

	return time.Unix(sec, nsec).UTC(), nil
}
