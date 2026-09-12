// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package scheduler

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/autobrr/netronome/internal/types"
)

func TestPacketLossSchedules(t *testing.T) {
	dbErr := errors.New("database unavailable")
	tests := []struct {
		name                  string
		disabled, nilNext     bool
		interval              string
		offset                time.Duration
		fetchErr, updateErr   error
		initWrites, runWrites int
		initNext, runNext     time.Duration
	}{
		{name: "disabled", disabled: true, interval: "1h"},
		{name: "nil next run", nilNext: true, interval: "1h", initWrites: 1, initNext: time.Hour},
		{name: "future", interval: "1h", offset: time.Hour},
		{name: "due now", interval: "1h", runWrites: 1, runNext: time.Hour},
		{name: "past", interval: "1h", offset: -10 * time.Minute, initWrites: 1, runWrites: 1, initNext: time.Hour, runNext: 50 * time.Minute},
		{name: "missed interval", interval: "1h", offset: -2 * time.Hour, initWrites: 1, runWrites: 1, initNext: time.Hour, runNext: time.Hour},
		{name: "invalid interval", interval: "bad", offset: -time.Hour},
		{name: "fetch error", fetchErr: dbErr},
		{name: "update error", interval: "1h", offset: -time.Minute, updateErr: dbErr, initWrites: 1, runWrites: 1, initNext: time.Hour, runNext: 59 * time.Minute},
	}
	for _, mode := range []string{"initialize", "run"} {
		for _, tt := range tests {
			t.Run(mode+"/"+tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					now := time.Now().UTC()
					last, next := now.Add(-3*time.Hour), now.Add(tt.offset).In(time.FixedZone("UTC+2", 2*60*60))
					monitor := types.PacketLossMonitor{ID: 17, Host: "packet.example", Enabled: !tt.disabled, Interval: tt.interval, LastRun: &last, NextRun: &next}
					if tt.nilNext {
						monitor.NextRun = nil
					}
					var updates []types.PacketLossMonitor
					reads := 0
					db := &inFlightDB{
						getPacketLossMonitors: func() ([]*types.PacketLossMonitor, error) {
							reads++
							copy := monitor
							return []*types.PacketLossMonitor{&copy}, tt.fetchErr
						},
						updatePacketLossMonitor: func(got *types.PacketLossMonitor) error {
							updates = append(updates, *got)
							return tt.updateErr
						},
					}
					s := New(db, nil, nil, nil, nil, nil).(*service)
					wantWrites, wantNext, wantLast := tt.initWrites, now.Add(tt.initNext), last
					if mode == "initialize" {
						s.initializePacketLossMonitors(context.Background())
					} else {
						// A nil concrete packet-loss service avoids performing network tests.
						s.checkAndRunPacketLossMonitors(context.Background())
						wantWrites, wantNext, wantLast = tt.runWrites, now.Add(tt.runNext), next.UTC()
					}
					synctest.Wait()
					if reads != 1 || len(updates) != wantWrites {
						t.Fatalf("reads = %d, updates = %v, want %d writes", reads, updates, wantWrites)
					}
					if wantWrites == 1 {
						want := monitor
						want.LastRun, want.NextRun = &wantLast, &wantNext
						if !reflect.DeepEqual(updates[0], want) {
							t.Errorf("update = %+v, want %+v", updates[0], want)
						}
					}
				})
			})
		}
	}
}

func TestUpdateMonitorSchedule(t *testing.T) {
	dbErr := errors.New("database unavailable")
	tests := []struct {
		name                string
		interval            string
		fetchErr, updateErr error
		wantErr             error
		writes              int
	}{
		{name: "valid", interval: "1h", writes: 1},
		{name: "invalid", interval: "invalid"},
		{name: "fetch error", interval: "1h", fetchErr: dbErr, wantErr: dbErr},
		{name: "update error", interval: "1h", updateErr: dbErr, wantErr: dbErr, writes: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now().UTC()
				var updates []types.PacketLossMonitor
				var ids []int64
				db := &inFlightDB{
					getPacketLossMonitor: func(id int64) (*types.PacketLossMonitor, error) {
						ids = append(ids, id)
						return &types.PacketLossMonitor{ID: id, Host: "packet.example", Enabled: true, Interval: "2h"}, tt.fetchErr
					},
					updatePacketLossMonitor: func(got *types.PacketLossMonitor) error {
						updates = append(updates, *got)
						return tt.updateErr
					},
				}
				err := New(db, nil, nil, nil, nil, nil).UpdateMonitorSchedule(42, tt.interval)
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("UpdateMonitorSchedule error = %v, want %v", err, tt.wantErr)
				}
				if !reflect.DeepEqual(ids, []int64{42}) || len(updates) != tt.writes {
					t.Fatalf("read IDs = %v, updates = %v, want %d writes", ids, updates, tt.writes)
				}
				if tt.writes == 1 {
					next := now.Add(time.Hour)
					want := types.PacketLossMonitor{ID: 42, Host: "packet.example", Enabled: true, Interval: "2h", LastRun: &now, NextRun: &next}
					if !reflect.DeepEqual(updates[0], want) {
						t.Errorf("update = %+v, want %+v", updates[0], want)
					}
				}
			})
		})
	}
}
