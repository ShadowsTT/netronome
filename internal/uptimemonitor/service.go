// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package uptimemonitor

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/netronome/internal/database"
	"github.com/autobrr/netronome/internal/notifications"
	"github.com/autobrr/netronome/internal/types"
)

// Monitor states. A monitor goes to StateDown on a failed check and to
// StateRecovered on the first success after that.
const (
	StateOK        = "ok"
	StateDown      = "down"
	StateRecovered = "recovered"
)

// Service runs uptime monitor checks and records what they find.
type Service struct {
	db        database.Service
	notifier  *notifications.Notifier
	mu        sync.RWMutex
	broadcast func(types.UptimeUpdate)
	// ponytail: in-memory cooldown, resets on restart
	lastCertNotification map[int64]time.Time
}

func NewService(db database.Service, notifier *notifications.Notifier) *Service {
	return &Service{db: db, notifier: notifier, lastCertNotification: make(map[int64]time.Time)}
}

// SetBroadcast sets the broadcast function for the service
func (s *Service) SetBroadcast(broadcast func(types.UptimeUpdate)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcast = broadcast
}

func (s *Service) send(update types.UptimeUpdate) {
	s.mu.RLock()
	broadcast := s.broadcast
	s.mu.RUnlock()

	if broadcast != nil {
		broadcast(update)
	}
}

// RunCheck probes the monitor once, saves the result, moves the monitor
// between states, and sends a notification on a state change.
func (s *Service) RunCheck(monitor *types.UptimeMonitor) {
	s.send(types.UptimeUpdate{
		Type:      "uptime",
		MonitorID: monitor.ID,
		Target:    monitor.Target,
		IsRunning: true,
		Enabled:   monitor.Enabled,
	})

	check := Probe(monitor)

	result := &types.UptimeResult{
		MonitorID:      monitor.ID,
		ResponseTimeMs: float64(check.ResponseTime.Microseconds()) / 1000,
		CertExpiry:     check.CertExpiry,
		Success:        check.Success,
		CreatedAt:      time.Now(),
	}
	if check.StatusCode != 0 {
		code := check.StatusCode
		result.StatusCode = &code
	}
	if check.Err != nil {
		errText := check.Err.Error()
		result.Error = &errText
	}

	log.Debug().
		Int64("monitorID", monitor.ID).
		Str("type", monitor.Type).
		Str("target", monitor.Target).
		Float64("responseTimeMs", result.ResponseTimeMs).
		Int("statusCode", check.StatusCode).
		Bool("success", result.Success).
		Msg("Uptime check completed")

	if err := s.db.SaveUptimeResult(result); err != nil {
		log.Error().Err(err).Int64("monitorID", monitor.ID).Msg("Failed to save uptime result")
	}

	state := s.applyState(monitor, check)
	s.sendCertNotification(monitor, check)

	update := types.UptimeUpdate{
		Type:           "uptime",
		MonitorID:      monitor.ID,
		Target:         monitor.Target,
		Enabled:        monitor.Enabled,
		Success:        result.Success,
		ResponseTimeMs: result.ResponseTimeMs,
		StatusCode:     check.StatusCode,
		CertExpiry:     check.CertExpiry,
		State:          state,
	}
	if result.Error != nil {
		update.Error = *result.Error
	}
	s.send(update)
}

func (s *Service) sendCertNotification(monitor *types.UptimeMonitor, check Check) {
	if !check.Success || check.CertExpiry == nil || s.notifier == nil {
		return
	}

	now := time.Now()
	s.mu.Lock()
	if now.Sub(s.lastCertNotification[monitor.ID]) < 24*time.Hour {
		s.mu.Unlock()
		return
	}
	s.lastCertNotification[monitor.ID] = now
	s.mu.Unlock()

	name := monitor.Name
	if name == "" {
		name = monitor.Target
	}
	daysLeft := math.Floor(time.Until(*check.CertExpiry).Hours() / 24)
	if err := s.notifier.SendUptimeCertNotification(name, monitor.Target, daysLeft); err != nil {
		s.mu.Lock()
		if s.lastCertNotification[monitor.ID] == now {
			delete(s.lastCertNotification, monitor.ID)
		}
		s.mu.Unlock()
		log.Error().Err(err).Int64("monitorID", monitor.ID).Msg("Failed to send uptime certificate notification")
	}
}

// applyState moves the monitor to its new state and notifies on a change. It
// returns the state the monitor is in after the check.
func (s *Service) applyState(monitor *types.UptimeMonitor, check Check) string {
	previous := monitor.LastState

	state := StateOK
	switch {
	case !check.Success:
		state = StateDown
	case previous == StateDown:
		state = StateRecovered
	}

	if state == previous {
		return state
	}

	if err := s.db.UpdateUptimeMonitorState(monitor.ID, state); err != nil {
		log.Error().Err(err).Int64("monitorID", monitor.ID).Str("state", state).Msg("Failed to update uptime monitor state")
	}
	monitor.LastState = state

	if s.notifier == nil || (state != StateDown && state != StateRecovered) {
		return state
	}

	name := monitor.Name
	if name == "" {
		name = monitor.Target
	}

	detail := fmt.Sprintf("Response time: %.0f ms", float64(check.ResponseTime.Microseconds())/1000)
	if check.Err != nil {
		detail = check.Err.Error()
	}

	if err := s.notifier.SendUptimeNotification(name, monitor.Target, detail, state == StateDown); err != nil {
		log.Error().Err(err).Int64("monitorID", monitor.ID).Str("state", state).Msg("Failed to send uptime notification")
	}

	return state
}

// GetMonitorStatus returns the last known status of a monitor.
func (s *Service) GetMonitorStatus(monitorID int64) (*types.UptimeUpdate, error) {
	monitor, err := s.db.GetUptimeMonitor(monitorID)
	if err != nil {
		return nil, err
	}

	update := &types.UptimeUpdate{
		Type:      "uptime",
		MonitorID: monitor.ID,
		Target:    monitor.Target,
		Enabled:   monitor.Enabled,
		State:     monitor.LastState,
	}

	result, err := s.db.GetLatestUptimeResult(monitorID)
	if err == database.ErrNotFound {
		return update, nil
	}
	if err != nil {
		return nil, err
	}

	update.Success = result.Success
	update.ResponseTimeMs = result.ResponseTimeMs
	update.CertExpiry = result.CertExpiry
	if result.StatusCode != nil {
		update.StatusCode = *result.StatusCode
	}
	if result.Error != nil {
		update.Error = *result.Error
	}

	return update, nil
}
