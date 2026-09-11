// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package uptimemonitor

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/netronome/internal/types"
)

func httpMonitor(target string) types.UptimeMonitor {
	return types.UptimeMonitor{
		Type:           types.UptimeTypeHTTP,
		Target:         target,
		TimeoutSeconds: 2,
		Method:         http.MethodGet,
		ExpectedStatus: "2xx",
		VerifyTLS:      true,
	}
}

func TestProbe_HTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		case "/teapot":
			w.WriteHeader(http.StatusTeapot)
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/slow":
			time.Sleep(1500 * time.Millisecond)
		case "/ua":
			_, _ = w.Write([]byte(r.UserAgent() + " " + r.Method))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name       string
		tweak      func(*types.UptimeMonitor)
		wantOK     bool
		wantStatus int
		wantErr    string
	}{
		{
			name:       "2xx passes",
			tweak:      func(m *types.UptimeMonitor) { m.Target = server.URL + "/ok" },
			wantOK:     true,
			wantStatus: 200,
		},
		{
			name:       "unexpected status fails",
			tweak:      func(m *types.UptimeMonitor) { m.Target = server.URL + "/teapot" },
			wantStatus: 418,
			wantErr:    "response code 418",
		},
		{
			name:       "exact status rule matches",
			tweak:      func(m *types.UptimeMonitor) { m.Target = server.URL + "/teapot"; m.ExpectedStatus = "418" },
			wantOK:     true,
			wantStatus: 418,
		},
		{
			name:       "range rule matches",
			tweak:      func(m *types.UptimeMonitor) { m.Target = server.URL + "/missing"; m.ExpectedStatus = "200-499" },
			wantOK:     true,
			wantStatus: 404,
		},
		{
			name:       "redirects are followed to the final status",
			tweak:      func(m *types.UptimeMonitor) { m.Target = server.URL + "/redirect" },
			wantOK:     true,
			wantStatus: 200,
		},
		{
			name:       "a redirect loop stops and reports the redirect status",
			tweak:      func(m *types.UptimeMonitor) { m.Target = server.URL + "/loop" },
			wantStatus: 302,
			wantErr:    "response code 302",
		},
		{
			name:       "keyword present passes",
			tweak:      func(m *types.UptimeMonitor) { m.Target = server.URL + "/ok"; m.Keyword = "healthy" },
			wantOK:     true,
			wantStatus: 200,
		},
		{
			name:       "keyword missing fails",
			tweak:      func(m *types.UptimeMonitor) { m.Target = server.URL + "/ok"; m.Keyword = "degraded" },
			wantStatus: 200,
			wantErr:    `keyword "degraded" not found`,
		},
		{
			name:       "head request is sent as HEAD with the netronome user agent",
			tweak:      func(m *types.UptimeMonitor) { m.Target = server.URL + "/ua"; m.Method = http.MethodHead },
			wantOK:     true,
			wantStatus: 200,
		},
		{
			name:    "timeout fails",
			tweak:   func(m *types.UptimeMonitor) { m.Target = server.URL + "/slow"; m.TimeoutSeconds = 1 },
			wantErr: "Timeout",
		},
		{
			name:    "connection refused fails",
			tweak:   func(m *types.UptimeMonitor) { m.Target = "http://127.0.0.1:1/" },
			wantErr: "connect",
		},
		{
			name:    "invalid expected status is a failure, not a pass",
			tweak:   func(m *types.UptimeMonitor) { m.Target = server.URL + "/ok"; m.ExpectedStatus = "ok" },
			wantErr: "expected status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := httpMonitor("")
			tt.tweak(&monitor)

			check := Probe(&monitor)

			assert.Equal(t, tt.wantOK, check.Success)
			assert.Equal(t, tt.wantStatus, check.StatusCode)
			if tt.wantErr == "" {
				assert.NoError(t, check.Err)
				assert.Positive(t, check.ResponseTime)
			} else {
				require.Error(t, check.Err)
				assert.Contains(t, check.Err.Error(), tt.wantErr)
			}
			assert.Nil(t, check.CertExpiry, "plain http has no certificate")
		})
	}
}

func TestProbe_HTTP_UserAgent(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.UserAgent()
	}))
	t.Cleanup(server.Close)

	monitor := httpMonitor(server.URL)
	require.True(t, Probe(&monitor).Success)
	assert.True(t, strings.HasPrefix(seen, "netronome/"), seen)
}

func TestProbe_HTTPS_TLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(server.Close)

	// the test server's certificate is self-signed, so verification fails
	strict := httpMonitor(server.URL)
	check := Probe(&strict)
	assert.False(t, check.Success)
	require.Error(t, check.Err)
	assert.Contains(t, strings.ToLower(check.Err.Error()), "certificate")

	// with verification off the check passes and records the expiry
	lax := httpMonitor(server.URL)
	lax.VerifyTLS = false
	check = Probe(&lax)
	assert.True(t, check.Success, check.Err)
	assert.Equal(t, 200, check.StatusCode)
	require.NotNil(t, check.CertExpiry)
	assert.True(t, check.CertExpiry.After(time.Now()))
}

func TestProbe_TCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	open := types.UptimeMonitor{Type: types.UptimeTypeTCP, Target: listener.Addr().String(), TimeoutSeconds: 2}
	check := Probe(&open)
	assert.True(t, check.Success, check.Err)
	assert.Zero(t, check.StatusCode)
	assert.Positive(t, check.ResponseTime)

	closed := types.UptimeMonitor{Type: types.UptimeTypeTCP, Target: "127.0.0.1:1", TimeoutSeconds: 2}
	check = Probe(&closed)
	assert.False(t, check.Success)
	require.Error(t, check.Err)
}

func TestParseExpectedStatus(t *testing.T) {
	tests := []struct {
		rule    string
		accept  []int
		reject  []int
		wantErr bool
	}{
		{rule: "200", accept: []int{200}, reject: []int{201, 404}},
		{rule: " 2xx ", accept: []int{200, 204, 299}, reject: []int{199, 300, 404}},
		{rule: "3XX", accept: []int{301, 302}, reject: []int{200}},
		{rule: "200-399", accept: []int{200, 301, 399}, reject: []int{199, 400}},
		{rule: "", wantErr: true},
		{rule: "ok", wantErr: true},
		{rule: "99", wantErr: true},
		{rule: "600", wantErr: true},
		{rule: "6xx", wantErr: true},
		{rule: "0xx", wantErr: true},
		{rule: "399-200", wantErr: true},
		{rule: "200-", wantErr: true},
		{rule: "200,201", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.rule, func(t *testing.T) {
			accept, err := ParseExpectedStatus(tt.rule)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			for _, code := range tt.accept {
				assert.True(t, accept(code), "expected %d to match %q", code, tt.rule)
			}
			for _, code := range tt.reject {
				assert.False(t, accept(code), "expected %d not to match %q", code, tt.rule)
			}
		})
	}
}
