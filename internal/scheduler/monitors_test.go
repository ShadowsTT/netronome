// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package scheduler

import (
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/autobrr/netronome/internal/types"
)

func TestDNSAndUptimeSchedules(t *testing.T) {
	dbErr := errors.New("database unavailable")
	tests := []struct {
		name                  string
		disabled, nilNext     bool
		interval              string
		offset, delay         time.Duration
		fetchErr, updateErr   error
		initWrites, runWrites int
		runs                  int
		initNext, runNext     time.Duration
	}{
		{name: "disabled", disabled: true, interval: "1h"},
		{name: "nil next run", nilNext: true, interval: "1h", initWrites: 1, initNext: time.Hour},
		{name: "future", interval: "1h", offset: time.Hour},
		{name: "due now", interval: "1h", initWrites: 1, runWrites: 1, runs: 1, initNext: time.Hour, runNext: time.Hour},
		{name: "past", interval: "1h", offset: -10 * time.Minute, initWrites: 1, runWrites: 1, runs: 1, initNext: time.Hour, runNext: 50 * time.Minute},
		{name: "missed interval", interval: "1h", offset: -2 * time.Hour, initWrites: 1, runWrites: 1, runs: 1, initNext: time.Hour, runNext: time.Hour},
		{name: "check overruns interval", interval: "1h", delay: 2 * time.Hour, initWrites: 1, runWrites: 1, runs: 1, initNext: time.Hour, runNext: 3 * time.Hour},
		{name: "invalid interval", interval: "bad", runs: 1},
		{name: "fetch error", fetchErr: dbErr},
		{name: "update error", interval: "1h", updateErr: dbErr, initWrites: 1, runWrites: 1, runs: 1, initNext: time.Hour, runNext: time.Hour},
	}
	for _, kind := range []string{"dns", "uptime"} {
		for _, mode := range []string{"initialize", "run"} {
			for _, tt := range tests {
				t.Run(kind+"/"+mode+"/"+tt.name, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						now := time.Now().UTC()
						last, next := now.Add(-3*time.Hour), now.Add(tt.offset).In(time.FixedZone("UTC+2", 2*60*60))
						nextRun := &next
						if tt.nilNext {
							nextRun = nil
						}
						type update struct {
							id   int64
							last *time.Time
							next time.Time
						}
						var updates []update
						var runIDs []int64
						reads := 0
						write := func(id int64, last *time.Time, next time.Time) error {
							updates = append(updates, update{id, last, next})
							return tt.updateErr
						}
						check := func(id int64) {
							runIDs = append(runIDs, id)
							time.Sleep(tt.delay)
						}
						db := &inFlightDB{}
						s := New(db, nil, nil, nil, nil, nil).(*service)
						var initialize, run func()
						if kind == "dns" {
							db.getDNSMonitors = func() ([]*types.DNSMonitor, error) {
								reads++
								return []*types.DNSMonitor{{ID: 23, Enabled: !tt.disabled, Interval: tt.interval, LastRun: &last, NextRun: nextRun}}, tt.fetchErr
							}
							db.updateDNSMonitorSchedule = write
							s.dns = monitorCheckFunc[types.DNSMonitor](func(m *types.DNSMonitor) { check(m.ID) })
							initialize, run = s.initializeDNSMonitors, s.checkAndRunDNSMonitors
						} else {
							db.getUptimeMonitors = func() ([]*types.UptimeMonitor, error) {
								reads++
								return []*types.UptimeMonitor{{ID: 23, Enabled: !tt.disabled, Interval: tt.interval, LastRun: &last, NextRun: nextRun}}, tt.fetchErr
							}
							db.updateUptimeMonitorSchedule = write
							s.uptime = monitorCheckFunc[types.UptimeMonitor](func(m *types.UptimeMonitor) { check(m.ID) })
							initialize, run = s.initializeUptimeMonitors, s.checkAndRunUptimeMonitors
						}
						wantWrites, wantRuns, wantLast, wantNext := tt.initWrites, 0, last, now.Add(tt.initNext)
						if mode == "initialize" {
							initialize()
						} else {
							run()
							synctest.Wait()
							time.Sleep(tt.delay)
							wantWrites, wantRuns, wantLast, wantNext = tt.runWrites, tt.runs, next.UTC(), now.Add(tt.runNext)
						}
						synctest.Wait()
						if reads != 1 || len(updates) != wantWrites || len(runIDs) != wantRuns {
							t.Fatalf("reads = %d, updates = %v, runs = %v; want %d writes, %d runs", reads, updates, runIDs, wantWrites, wantRuns)
						}
						if wantRuns == 1 && runIDs[0] != 23 {
							t.Errorf("checked ID = %d, want 23", runIDs[0])
						}
						if wantWrites == 1 {
							want := update{23, &wantLast, wantNext}
							if !reflect.DeepEqual(updates[0], want) {
								t.Errorf("update = %+v, want %+v", updates[0], want)
							}
						}
					})
				})
			}
		}
	}
}
