// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/netronome/internal/types"
)

func TestNormalizeUptimeMonitor(t *testing.T) {
	tests := []struct {
		name    string
		input   types.UptimeMonitor
		want    types.UptimeMonitor
		wantErr string
	}{
		{
			name:  "fills in the http defaults",
			input: types.UptimeMonitor{Target: "https://example.invalid/health", VerifyTLS: true},
			want:  types.UptimeMonitor{Type: "http", Target: "https://example.invalid/health", Interval: "60s", TimeoutSeconds: 10, Method: "GET", ExpectedStatus: "2xx", VerifyTLS: true},
		},
		{
			name:  "trims and normalises the given http values",
			input: types.UptimeMonitor{Name: " Web ", Type: "HTTP", Target: " http://10.0.0.5:8080/ ", Interval: "5m", TimeoutSeconds: 30, Method: "head", ExpectedStatus: " 200-399 "},
			want:  types.UptimeMonitor{Name: "Web", Type: "http", Target: "http://10.0.0.5:8080/", Interval: "5m", TimeoutSeconds: 30, Method: "HEAD", ExpectedStatus: "200-399"},
		},
		{
			name:  "keeps a keyword with GET",
			input: types.UptimeMonitor{Target: "http://example.invalid/", Keyword: " healthy "},
			want:  types.UptimeMonitor{Type: "http", Target: "http://example.invalid/", Interval: "60s", TimeoutSeconds: 10, Method: "GET", ExpectedStatus: "2xx", Keyword: "healthy"},
		},
		{
			name:  "tcp keeps host:port and resets the http fields",
			input: types.UptimeMonitor{Type: "tcp", Target: " 192.168.1.1:22 ", Method: "HEAD", ExpectedStatus: "500", Keyword: "x"},
			want:  types.UptimeMonitor{Type: "tcp", Target: "192.168.1.1:22", Interval: "60s", TimeoutSeconds: 10, Method: "GET", ExpectedStatus: "2xx"},
		},
		{
			name:  "tcp accepts a bracketed ipv6 target",
			input: types.UptimeMonitor{Type: "tcp", Target: "[::1]:443"},
			want:  types.UptimeMonitor{Type: "tcp", Target: "[::1]:443", Interval: "60s", TimeoutSeconds: 10, Method: "GET", ExpectedStatus: "2xx"},
		},
		{
			name:    "rejects an empty target",
			input:   types.UptimeMonitor{Target: "   "},
			wantErr: "Target is required",
		},
		{
			name:    "rejects an unknown type",
			input:   types.UptimeMonitor{Type: "icmp", Target: "1.1.1.1"},
			wantErr: "Type must be one of: http, tcp",
		},
		{
			name:    "rejects a non-http scheme",
			input:   types.UptimeMonitor{Target: "ftp://example.invalid/"},
			wantErr: "Target must be an http:// or https:// URL",
		},
		{
			name:    "rejects a bare host for http",
			input:   types.UptimeMonitor{Target: "example.invalid"},
			wantErr: "Target must be an http:// or https:// URL",
		},
		{
			name:    "rejects an unknown method",
			input:   types.UptimeMonitor{Target: "http://example.invalid/", Method: "POST"},
			wantErr: "Method must be one of: GET, HEAD",
		},
		{
			name:    "rejects a bad expected status",
			input:   types.UptimeMonitor{Target: "http://example.invalid/", ExpectedStatus: "ok"},
			wantErr: "Expected status must be",
		},
		{
			name:    "rejects a keyword with HEAD",
			input:   types.UptimeMonitor{Target: "http://example.invalid/", Method: "HEAD", Keyword: "x"},
			wantErr: "Keyword needs the GET method",
		},
		{
			name:    "rejects a tcp target without a port",
			input:   types.UptimeMonitor{Type: "tcp", Target: "192.168.1.1"},
			wantErr: "Target must be host:port",
		},
		{
			name:    "rejects a tcp target with a bad port",
			input:   types.UptimeMonitor{Type: "tcp", Target: "192.168.1.1:70000"},
			wantErr: "Port must be between 1 and 65535",
		},
		{
			name:    "rejects a timeout above the limit",
			input:   types.UptimeMonitor{Target: "http://example.invalid/", TimeoutSeconds: 121},
			wantErr: "Timeout must be between 1 and 120 seconds",
		},
		{
			name:    "rejects a negative timeout",
			input:   types.UptimeMonitor{Target: "http://example.invalid/", TimeoutSeconds: -1},
			wantErr: "Timeout must be between 1 and 120 seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := tt.input
			err := normalizeUptimeMonitor(&monitor)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, monitor)
		})
	}
}

func TestUptimeMonitorRequest_PointerDefaults(t *testing.T) {
	f := false

	// a body without the fields keeps the fallbacks
	monitor := uptimeMonitorRequest{Target: "http://example.invalid/"}.monitor(true, true)
	assert.True(t, monitor.Enabled)
	assert.True(t, monitor.VerifyTLS)

	// a body that sends false turns them off
	monitor = uptimeMonitorRequest{Target: "http://example.invalid/", Enabled: &f, VerifyTLS: &f}.monitor(true, true)
	assert.False(t, monitor.Enabled)
	assert.False(t, monitor.VerifyTLS)
}
