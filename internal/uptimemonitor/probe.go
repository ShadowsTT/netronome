// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package uptimemonitor checks that an HTTP URL or a TCP port answers, on a
// schedule, and records the response time. It measures reachability, not
// content: an HTTP check passes on the expected status code (and a keyword in
// the body if one is set), a TCP check passes when the connection opens.
package uptimemonitor

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/netronome/internal/types"
	"github.com/autobrr/netronome/internal/version"
)

// Limits on what one check may do.
const (
	MinTimeoutSeconds = 1
	MaxTimeoutSeconds = 120
	DefaultTimeout    = 10

	// maxBodyBytes caps how much of a response body a keyword search reads.
	maxBodyBytes = 1 << 20

	// maxRedirects is how many hops an HTTP check follows before it reports the
	// redirect itself as the response.
	maxRedirects = 5
)

// Methods an HTTP check can use. HEAD cannot carry a keyword check.
var Methods = []string{http.MethodGet, http.MethodHead}

// Check is the outcome of one uptime check.
type Check struct {
	ResponseTime time.Duration
	StatusCode   int
	CertExpiry   *time.Time
	Success      bool
	Err          error
}

// Probe runs one check against the monitor's target.
func Probe(monitor *types.UptimeMonitor) Check {
	if monitor.Type == types.UptimeTypeTCP {
		return probeTCP(monitor)
	}
	return probeHTTP(monitor)
}

func timeout(monitor *types.UptimeMonitor) time.Duration {
	seconds := monitor.TimeoutSeconds
	if seconds < MinTimeoutSeconds || seconds > MaxTimeoutSeconds {
		seconds = DefaultTimeout
	}
	return time.Duration(seconds) * time.Second
}

func probeTCP(monitor *types.UptimeMonitor) Check {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", monitor.Target, timeout(monitor))
	elapsed := time.Since(start)
	if err != nil {
		return Check{ResponseTime: elapsed, Err: err}
	}
	_ = conn.Close()

	return Check{ResponseTime: elapsed, Success: true}
}

func probeHTTP(monitor *types.UptimeMonitor) Check {
	accept, err := ParseExpectedStatus(monitor.ExpectedStatus)
	if err != nil {
		return Check{Err: err}
	}

	method := strings.ToUpper(monitor.Method)
	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequest(method, monitor.Target, nil)
	if err != nil {
		return Check{Err: err}
	}
	req.Header.Set("User-Agent", "netronome/"+version.Version)

	client := &http.Client{
		Timeout: timeout(monitor),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
		Transport: &http.Transport{
			// the user opts out of verification per monitor, for self-signed
			// lab hosts
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: !monitor.VerifyTLS}, //nolint:gosec
			DisableKeepAlives: true,
			Proxy:             http.ProxyFromEnvironment,
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return Check{ResponseTime: elapsed, Err: err}
	}
	defer resp.Body.Close()

	check := Check{ResponseTime: elapsed, StatusCode: resp.StatusCode}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		expiry := resp.TLS.PeerCertificates[0].NotAfter
		check.CertExpiry = &expiry
	}

	if !accept(resp.StatusCode) {
		check.Err = fmt.Errorf("response code %d", resp.StatusCode)
		return check
	}

	if monitor.Keyword != "" {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if err != nil {
			check.Err = fmt.Errorf("read body: %w", err)
			return check
		}
		if !strings.Contains(string(body), monitor.Keyword) {
			check.Err = fmt.Errorf("keyword %q not found in body", monitor.Keyword)
			return check
		}
	}

	check.Success = true
	return check
}

// ParseExpectedStatus turns a status rule into a predicate. A rule is one
// exact code ("200"), one class ("2xx"), or one inclusive range ("200-399").
func ParseExpectedStatus(rule string) (func(int) bool, error) {
	rule = strings.ToLower(strings.TrimSpace(rule))
	invalid := errors.New("expected status must be a code (200), a class (2xx), or a range (200-399)")

	if lo, hi, ok := strings.Cut(rule, "-"); ok {
		from, err1 := strconv.Atoi(lo)
		to, err2 := strconv.Atoi(hi)
		if err1 != nil || err2 != nil || !isStatusCode(from) || !isStatusCode(to) || from > to {
			return nil, invalid
		}
		return func(code int) bool { return code >= from && code <= to }, nil
	}

	if len(rule) == 3 && strings.HasSuffix(rule, "xx") {
		class, err := strconv.Atoi(rule[:1])
		if err != nil || class < 1 || class > 5 {
			return nil, invalid
		}
		return func(code int) bool { return code/100 == class }, nil
	}

	exact, err := strconv.Atoi(rule)
	if err != nil || !isStatusCode(exact) {
		return nil, invalid
	}
	return func(code int) bool { return code == exact }, nil
}

func isStatusCode(code int) bool {
	return code >= 100 && code <= 599
}
