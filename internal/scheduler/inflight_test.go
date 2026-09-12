// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/autobrr/netronome/internal/database"
	"github.com/autobrr/netronome/internal/types"
)

type monitorCheckFunc[T any] func(*T)

func (f monitorCheckFunc[T]) RunCheck(monitor *T) { f(monitor) }

// Unless overridden, return due DNS and uptime monitors to exercise dispatch guards.
type inFlightDB struct {
	database.Service
	nextRun                     time.Time
	getSchedules                func(context.Context) ([]types.Schedule, error)
	updateSchedule              func(context.Context, types.Schedule) error
	getPacketLossMonitors       func() ([]*types.PacketLossMonitor, error)
	getPacketLossMonitor        func(int64) (*types.PacketLossMonitor, error)
	updatePacketLossMonitor     func(*types.PacketLossMonitor) error
	getDNSMonitors              func() ([]*types.DNSMonitor, error)
	getUptimeMonitors           func() ([]*types.UptimeMonitor, error)
	updateDNSMonitorSchedule    func(int64, *time.Time, time.Time) error
	updateUptimeMonitorSchedule func(int64, *time.Time, time.Time) error
}

func (db *inFlightDB) GetSchedules(ctx context.Context) ([]types.Schedule, error) {
	if db.getSchedules != nil {
		return db.getSchedules(ctx)
	}
	return nil, nil
}

func (db *inFlightDB) UpdateSchedule(ctx context.Context, schedule types.Schedule) error {
	return db.updateSchedule(ctx, schedule)
}

func (db *inFlightDB) GetPacketLossMonitors() ([]*types.PacketLossMonitor, error) {
	if db.getPacketLossMonitors != nil {
		return db.getPacketLossMonitors()
	}
	return nil, nil
}

func (db *inFlightDB) GetPacketLossMonitor(id int64) (*types.PacketLossMonitor, error) {
	return db.getPacketLossMonitor(id)
}

func (db *inFlightDB) UpdatePacketLossMonitor(monitor *types.PacketLossMonitor) error {
	return db.updatePacketLossMonitor(monitor)
}

func (db *inFlightDB) GetDNSMonitors() ([]*types.DNSMonitor, error) {
	if db.getDNSMonitors != nil {
		return db.getDNSMonitors()
	}
	return []*types.DNSMonitor{{ID: 1, Enabled: true, Interval: "1m", NextRun: &db.nextRun}}, nil
}

func (db *inFlightDB) GetUptimeMonitors() ([]*types.UptimeMonitor, error) {
	if db.getUptimeMonitors != nil {
		return db.getUptimeMonitors()
	}
	return []*types.UptimeMonitor{{ID: 1, Enabled: true, Interval: "1m", NextRun: &db.nextRun}}, nil
}

func (db *inFlightDB) UpdateDNSMonitorSchedule(id int64, last *time.Time, next time.Time) error {
	if db.updateDNSMonitorSchedule != nil {
		return db.updateDNSMonitorSchedule(id, last, next)
	}
	return nil
}

func (db *inFlightDB) UpdateUptimeMonitorSchedule(id int64, last *time.Time, next time.Time) error {
	if db.updateUptimeMonitorSchedule != nil {
		return db.updateUptimeMonitorSchedule(id, last, next)
	}
	return nil
}

func TestMonitorInFlight(t *testing.T) {
	tests := []struct {
		name string
		run  func(*service)
		want int64
	}{
		{name: "dns", run: (*service).checkAndRunDNSMonitors, want: 1},
		{name: "uptime", run: (*service).checkAndRunUptimeMonitors, want: 1},
		{name: "dns and uptime with the same ID", run: func(s *service) {
			s.checkAndRunDNSMonitors()
			s.checkAndRunUptimeMonitors()
		}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls atomic.Int64
				release := make(chan struct{})
				defer func() {
					close(release)
					synctest.Wait()
				}()
				check := func() {
					calls.Add(1)
					<-release
				}
				s := &service{
					db:     &inFlightDB{nextRun: time.Now().Add(-time.Minute)},
					dns:    monitorCheckFunc[types.DNSMonitor](func(*types.DNSMonitor) { check() }),
					uptime: monitorCheckFunc[types.UptimeMonitor](func(*types.UptimeMonitor) { check() }),
				}

				tt.run(s)
				synctest.Wait()
				tt.run(s)
				synctest.Wait()
				if got := calls.Load(); got != tt.want {
					t.Fatalf("RunCheck calls while blocked = %d, want %d", got, tt.want)
				}

				for range tt.want {
					release <- struct{}{}
				}
				synctest.Wait()
				tt.run(s)
				synctest.Wait()
				if got := calls.Load(); got != 2*tt.want {
					t.Fatalf("RunCheck calls after completion = %d, want %d", got, 2*tt.want)
				}
			})
		})
	}
}

func TestMonitorInFlightNilServices(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, nil).(*service)
	s.initializeDNSMonitors()
	s.initializeUptimeMonitors()
	s.checkAndRunDNSMonitors()
	s.checkAndRunUptimeMonitors()
}
