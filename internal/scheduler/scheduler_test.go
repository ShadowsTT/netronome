// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package scheduler

import (
	"strings"
	"testing"
	"time"
)

func TestCalculateNextRun(t *testing.T) {
	from := time.Date(2026, time.January, 31, 14, 0, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	tests := []struct {
		interval string
		want     time.Duration
		invalid  bool
	}{
		{interval: "1h", want: time.Hour},
		{interval: "1m", want: time.Minute},
		{interval: "60s", want: time.Minute},
		{interval: "3600s", want: time.Hour},
		{interval: "2d", want: 48 * time.Hour},
		{interval: "1w", want: 7 * 24 * time.Hour},
		{interval: "exact:13:00", want: time.Hour},
		{interval: "exact:12:00", want: 24 * time.Hour},
		{interval: "exact:00:00", want: 12 * time.Hour},
		{interval: "exact:20:00, 13:00,09:00,15:00", want: time.Hour},
		{interval: "exact:bad,24:00,xx:00,12:60,12:xx,13:00", want: time.Hour},
		{interval: "exact:", invalid: true},
		{interval: "exact:bad", invalid: true},
		{interval: "exact:-1:00", invalid: true},
		{interval: "exact:24:00", invalid: true},
		{interval: "exact:12:-1", invalid: true},
		{interval: "exact:12:60", invalid: true},
		{interval: "invalid", invalid: true},
		{interval: "", invalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.interval, func(t *testing.T) {
			s := New(nil, nil, nil, nil, nil, nil)
			precise := s.(*service).calculateNextRun(tt.interval, from, true)
			jittered := s.CalculateNextRun(tt.interval, from)
			if tt.invalid {
				if !precise.IsZero() || !jittered.IsZero() {
					t.Fatalf("invalid interval: precise = %v, jittered = %v", precise, jittered)
				}
				return
			}
			want := from.UTC().Add(tt.want)
			if !precise.Equal(want) || precise.Location() != time.UTC {
				t.Errorf("precise = %v, want %v in UTC", precise, want)
			}
			maxJitter := 5 * time.Minute
			if strings.HasPrefix(tt.interval, "exact:") {
				maxJitter = time.Minute
			}
			if jitter := jittered.Sub(want); jitter < time.Second || jitter > maxJitter || jittered.Location() != time.UTC {
				t.Errorf("jittered = %v, want %v + [1s, %v] in UTC", jittered, want, maxJitter)
			}
		})
	}
}

func TestIsValidScheduleInterval(t *testing.T) {
	tests := []struct {
		interval string
		want     bool
	}{
		{"1h", true}, {"60s", true}, {"1m", true}, {"2d", true}, {"1w", true},
		{"exact:14:00", true}, {"exact:00:00, 09:00,23:59", true},
		{"invalid", false}, {"", false}, {"exact:", false},
		{"exact:12", false}, {"exact:12:00:00", false},
		{"exact:xx:00", false}, {"exact:-1:00", false}, {"exact:24:00", false},
		{"exact:12:xx", false}, {"exact:12:-1", false}, {"exact:12:60", false},
		{"exact:12:00,", false}, {"exact:12:00,invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.interval, func(t *testing.T) {
			if got := (&service{}).isValidScheduleInterval(tt.interval); got != tt.want {
				t.Errorf("isValidScheduleInterval(%q) = %v, want %v", tt.interval, got, tt.want)
			}
		})
	}
}

func TestNormalizeDuration(t *testing.T) {
	tests := []struct{ interval, want string }{
		{"1d", "24h"}, {"2w", "336h"}, {"-1d", "-24h"}, {"0w", "0h"},
		{"d", "d"}, {"w", "w"}, {"xd", "xd"}, {"xw", "xw"},
		{"1.5d", "1.5d"}, {"1.5w", "1.5w"}, {"1h30m", "1h30m"}, {"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.interval, func(t *testing.T) {
			if got := (&service{}).normalizeDuration(tt.interval); got != tt.want {
				t.Errorf("normalizeDuration(%q) = %q, want %q", tt.interval, got, tt.want)
			}
		})
	}
}
