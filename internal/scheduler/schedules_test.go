// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package scheduler

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/autobrr/netronome/internal/dnsmonitor"
	"github.com/autobrr/netronome/internal/speedtest"
	"github.com/autobrr/netronome/internal/types"
	"github.com/autobrr/netronome/internal/uptimemonitor"
)

type scheduledSpeedtest struct {
	speedtest.Service
	run func(context.Context, *types.TestOptions) (*speedtest.Result, error)
}

func (s scheduledSpeedtest) RunTest(ctx context.Context, options *types.TestOptions) (*speedtest.Result, error) {
	return s.run(ctx, options)
}

func TestInitializeSchedules(t *testing.T) {
	dbErr := errors.New("database unavailable")
	tests := []struct {
		name      string
		enabled   bool
		interval  string
		offset    time.Duration
		fetchErr  error
		updateErr error
		wantWrite int
	}{
		{name: "past", enabled: true, interval: "1h", offset: -time.Hour, wantWrite: 1},
		{name: "disabled", interval: "1h", offset: -time.Hour},
		{name: "future", enabled: true, interval: "1h", offset: time.Hour},
		{name: "equal to now", enabled: true, interval: "1h"},
		{name: "invalid", enabled: true, interval: "bad", offset: -time.Hour},
		{name: "fetch error", fetchErr: dbErr},
		{name: "update error", enabled: true, interval: "1h", offset: -time.Hour, updateErr: dbErr, wantWrite: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now().UTC()
				last := now.Add(-2 * time.Hour)
				original := types.Schedule{ID: 7, Enabled: tt.enabled, Interval: tt.interval, LastRun: &last, NextRun: now.Add(tt.offset)}
				var updates []types.Schedule
				reads := 0
				ctx := context.Background()
				db := &inFlightDB{
					getSchedules: func(got context.Context) ([]types.Schedule, error) {
						reads++
						if got != ctx {
							t.Error("fetch did not receive caller context")
						}
						return []types.Schedule{original}, tt.fetchErr
					},
					updateSchedule: func(got context.Context, schedule types.Schedule) error {
						if got != ctx {
							t.Error("update did not receive caller context")
						}
						updates = append(updates, schedule)
						return tt.updateErr
					},
				}
				New(db, nil, nil, nil, nil, nil).(*service).initializeSchedules(ctx)
				if reads != 1 || len(updates) != tt.wantWrite {
					t.Fatalf("reads = %d, updates = %v, want write = %v", reads, updates, tt.wantWrite)
				}
				if tt.wantWrite == 1 {
					got := updates[0]
					assertJitteredNextRun(t, got.NextRun, now.Add(time.Hour))
					want := original
					want.NextRun = got.NextRun
					if !reflect.DeepEqual(got, want) {
						t.Errorf("update = %+v, want %+v", got, want)
					}
				}
			})
		})
	}
}

func TestCheckAndRunScheduledTests(t *testing.T) {
	testErr := errors.New("test failure")
	tests := []struct {
		name                        string
		offset                      time.Duration
		zero, disabled, invalid     bool
		fetchErr, runErr, updateErr error
		wantRun, wantWrite          int
		fromNow                     bool
	}{
		{name: "due now", wantRun: 1, wantWrite: 1},
		{name: "scheduled start", offset: -10 * time.Minute, wantRun: 1, wantWrite: 1},
		{name: "zero next run", zero: true, wantRun: 1, wantWrite: 1, fromNow: true},
		{name: "missed interval", offset: -2 * time.Hour, wantRun: 1, wantWrite: 1, fromNow: true},
		{name: "disabled", disabled: true},
		{name: "future", offset: time.Hour},
		{name: "fetch error", fetchErr: testErr},
		{name: "run error", runErr: testErr, wantRun: 1},
		{name: "invalid interval", invalid: true, wantRun: 1},
		{name: "update error", updateErr: testErr, wantRun: 1, wantWrite: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now().UTC()
				schedule := types.Schedule{ID: 9, Enabled: !tt.disabled, Interval: "1h", NextRun: now.Add(tt.offset), Options: types.TestOptions{EnableDownload: true, UseIperf: true, ServerHost: "iperf.example"}}
				if tt.zero {
					schedule.NextRun = time.Time{}
				}
				if tt.invalid {
					schedule.Interval = "invalid"
				}
				var updates []types.Schedule
				reads, runs := 0, 0
				var runCtx context.Context
				db := &inFlightDB{
					getSchedules: func(context.Context) ([]types.Schedule, error) {
						reads++
						return []types.Schedule{schedule}, tt.fetchErr
					},
					updateSchedule: func(ctx context.Context, got types.Schedule) error {
						if ctx != runCtx {
							t.Error("update did not receive run context")
						}
						updates = append(updates, got)
						return tt.updateErr
					},
				}
				runner := scheduledSpeedtest{run: func(ctx context.Context, opts *types.TestOptions) (*speedtest.Result, error) {
					runs++
					runCtx = ctx
					want := schedule.Options
					want.IsScheduled = true
					if !reflect.DeepEqual(*opts, want) {
						t.Errorf("options = %+v, want %+v", opts, want)
					}
					if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(now.Add(5*time.Minute)) {
						t.Errorf("deadline = %v, present = %v", deadline, ok)
					}
					return &speedtest.Result{DownloadSpeed: 10, UploadSpeed: 5}, tt.runErr
				}}
				New(db, runner, nil, nil, nil, nil).(*service).checkAndRunScheduledTests(context.Background())
				synctest.Wait()
				if reads != 1 || runs != tt.wantRun || len(updates) != tt.wantWrite {
					t.Fatalf("reads = %d, runs = %d, updates = %v; want run = %v, write = %v", reads, runs, updates, tt.wantRun, tt.wantWrite)
				}
				if runCtx != nil && !errors.Is(runCtx.Err(), context.Canceled) {
					t.Errorf("run context was not canceled: %v", runCtx.Err())
				}
				if tt.wantWrite == 1 {
					base := schedule.NextRun
					if tt.fromNow {
						base = now
					}
					got := updates[0]
					assertJitteredNextRun(t, got.NextRun, base.Add(time.Hour))
					if got.ID != schedule.ID || got.LastRun == nil || !got.LastRun.Equal(now) || !got.Options.IsScheduled {
						t.Errorf("unexpected update: %+v", got)
					}
				}
			})
		})
	}
}

func assertJitteredNextRun(t *testing.T, got, base time.Time) {
	t.Helper()
	if jitter := got.Sub(base); jitter < time.Second || jitter > 5*time.Minute || got.Location() != time.UTC {
		t.Errorf("next run = %v, want %v + [1s, 5m] in UTC", got, base)
	}
}

func TestSchedulerStartStop(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "empty database"},
		{name: "fetch errors", err: errors.New("fetch failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var reads [4]atomic.Int64
				db := &inFlightDB{
					getSchedules:          func(context.Context) ([]types.Schedule, error) { reads[0].Add(1); return nil, tt.err },
					getPacketLossMonitors: func() ([]*types.PacketLossMonitor, error) { reads[1].Add(1); return nil, tt.err },
					getDNSMonitors:        func() ([]*types.DNSMonitor, error) { reads[2].Add(1); return nil, tt.err },
					getUptimeMonitors:     func() ([]*types.UptimeMonitor, error) { reads[3].Add(1); return nil, tt.err },
				}
				scheduler := New(db, nil, nil, &dnsmonitor.Service{}, &uptimemonitor.Service{}, nil)
				defer func() { scheduler.Stop(); synctest.Wait() }()
				scheduler.Stop() // Safe before Start.
				for cycle := 1; cycle <= 2; cycle++ {
					scheduler.Start(context.Background())
					scheduler.Start(context.Background()) // Duplicate Start must not create another ticker.
					synctest.Wait()
					want := int64(2*cycle - 1)
					if got := [4]int64{reads[0].Load(), reads[1].Load(), reads[2].Load(), reads[3].Load()}; got != [4]int64{want, want, want, want} {
						t.Fatalf("initialization reads = %v, want %d each", got, want)
					}
					time.Sleep(time.Minute)
					synctest.Wait()
					scheduler.Stop()
					synctest.Wait()
					scheduler.Stop()
					time.Sleep(2 * time.Minute)
					synctest.Wait()
					want = int64(2 * cycle)
					if got := [4]int64{reads[0].Load(), reads[1].Load(), reads[2].Load(), reads[3].Load()}; got != [4]int64{want, want, want, want} {
						t.Fatalf("reads after tick and stop = %v, want %d each", got, want)
					}
				}
			})
		})
	}
}
