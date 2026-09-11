// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/netronome/internal/config"
	"github.com/autobrr/netronome/internal/types"
)

var uptimeMonitorColumns = []string{
	"id", "name", "type", "target", "interval", "timeout_seconds", "method", "expected_status", "keyword", "verify_tls", "enabled",
	"last_run", "next_run", "last_state", "last_state_change", "created_at", "updated_at",
}

var uptimeResultColumns = []string{
	"id", "monitor_id", "response_time_ms", "status_code", "cert_expiry", "success", "error", "created_at",
}

func scanUptimeMonitor(scan func(dest ...any) error) (*types.UptimeMonitor, error) {
	monitor := &types.UptimeMonitor{}
	err := scan(
		&monitor.ID,
		&monitor.Name,
		&monitor.Type,
		&monitor.Target,
		&monitor.Interval,
		&monitor.TimeoutSeconds,
		&monitor.Method,
		&monitor.ExpectedStatus,
		&monitor.Keyword,
		&monitor.VerifyTLS,
		&monitor.Enabled,
		&monitor.LastRun,
		&monitor.NextRun,
		&monitor.LastState,
		&monitor.LastStateChange,
		&monitor.CreatedAt,
		&monitor.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return monitor, nil
}

func scanUptimeResult(scan func(dest ...any) error) (*types.UptimeResult, error) {
	result := &types.UptimeResult{}
	err := scan(
		&result.ID,
		&result.MonitorID,
		&result.ResponseTimeMs,
		&result.StatusCode,
		&result.CertExpiry,
		&result.Success,
		&result.Error,
		&result.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// CreateUptimeMonitor creates a new uptime monitor
func (s *service) CreateUptimeMonitor(monitor *types.UptimeMonitor) (*types.UptimeMonitor, error) {
	monitor.CreatedAt = time.Now()
	monitor.UpdatedAt = time.Now()

	query := s.sqlBuilder.
		Insert("uptime_monitors").
		Columns("name", "type", "target", "interval", "timeout_seconds", "method", "expected_status", "keyword", "verify_tls", "enabled", "next_run", "created_at", "updated_at").
		Values(monitor.Name, monitor.Type, monitor.Target, monitor.Interval, monitor.TimeoutSeconds, monitor.Method, monitor.ExpectedStatus, monitor.Keyword, monitor.VerifyTLS, monitor.Enabled, monitor.NextRun, monitor.CreatedAt, monitor.UpdatedAt)

	if s.config.Type == config.Postgres {
		query = query.Suffix("RETURNING id")
		if err := query.RunWith(s.db).QueryRow().Scan(&monitor.ID); err != nil {
			return nil, fmt.Errorf("failed to create uptime monitor: %w", err)
		}
		return monitor, nil
	}

	res, err := query.RunWith(s.db).Exec()
	if err != nil {
		return nil, fmt.Errorf("failed to create uptime monitor: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert ID: %w", err)
	}
	monitor.ID = id

	return monitor, nil
}

// GetUptimeMonitor retrieves an uptime monitor by ID
func (s *service) GetUptimeMonitor(monitorID int64) (*types.UptimeMonitor, error) {
	query := s.sqlBuilder.
		Select(uptimeMonitorColumns...).
		From("uptime_monitors").
		Where(sq.Eq{"id": monitorID})

	monitor, err := scanUptimeMonitor(query.RunWith(s.db).QueryRow().Scan)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get uptime monitor: %w", err)
	}

	return monitor, nil
}

// GetUptimeMonitors retrieves all uptime monitors
func (s *service) GetUptimeMonitors() ([]*types.UptimeMonitor, error) {
	query := s.sqlBuilder.
		Select(uptimeMonitorColumns...).
		From("uptime_monitors").
		OrderBy("created_at DESC")

	rows, err := query.RunWith(s.db).Query()
	if err != nil {
		return nil, fmt.Errorf("failed to get uptime monitors: %w", err)
	}
	defer rows.Close()

	var monitors []*types.UptimeMonitor
	for rows.Next() {
		monitor, err := scanUptimeMonitor(rows.Scan)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan uptime monitor")
			continue
		}
		monitors = append(monitors, monitor)
	}

	return monitors, rows.Err()
}

// UpdateUptimeMonitor updates an existing uptime monitor
func (s *service) UpdateUptimeMonitor(monitor *types.UptimeMonitor) error {
	monitor.UpdatedAt = time.Now()

	query := s.sqlBuilder.
		Update("uptime_monitors").
		SetMap(map[string]interface{}{
			"name":            monitor.Name,
			"type":            monitor.Type,
			"target":          monitor.Target,
			"interval":        monitor.Interval,
			"timeout_seconds": monitor.TimeoutSeconds,
			"method":          monitor.Method,
			"expected_status": monitor.ExpectedStatus,
			"keyword":         monitor.Keyword,
			"verify_tls":      monitor.VerifyTLS,
			"enabled":         monitor.Enabled,
			"last_run":        monitor.LastRun,
			"next_run":        monitor.NextRun,
			"updated_at":      monitor.UpdatedAt,
		}).
		Where(sq.Eq{"id": monitor.ID})

	return s.execOne(query, "update uptime monitor")
}

// UpdateUptimeMonitorSchedule writes only the run times, so a user edit made
// while a check ran is not overwritten by the scheduler's stale copy.
func (s *service) UpdateUptimeMonitorSchedule(monitorID int64, lastRun *time.Time, nextRun time.Time) error {
	query := s.sqlBuilder.
		Update("uptime_monitors").
		Set("last_run", lastRun).
		Set("next_run", nextRun).
		Where(sq.Eq{"id": monitorID})

	return s.execOne(query, "update uptime monitor schedule")
}

// UpdateUptimeMonitorState updates the monitor state and timestamp
func (s *service) UpdateUptimeMonitorState(monitorID int64, state string) error {
	query := s.sqlBuilder.
		Update("uptime_monitors").
		Set("last_state", state).
		Set("last_state_change", time.Now()).
		Where(sq.Eq{"id": monitorID})

	return s.execOne(query, "update uptime monitor state")
}

// execOne runs an update that must touch exactly one row.
func (s *service) execOne(query sq.UpdateBuilder, action string) error {
	res, err := query.RunWith(s.db).Exec()
	if err != nil {
		return fmt.Errorf("failed to %s: %w", action, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteUptimeMonitor deletes an uptime monitor and its results
func (s *service) DeleteUptimeMonitor(monitorID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// delete results first, SQLite only enforces the cascade with foreign keys on
	if _, err := s.sqlBuilder.Delete("uptime_results").Where(sq.Eq{"monitor_id": monitorID}).RunWith(tx).Exec(); err != nil {
		return fmt.Errorf("failed to delete uptime results: %w", err)
	}

	res, err := s.sqlBuilder.Delete("uptime_monitors").Where(sq.Eq{"id": monitorID}).RunWith(tx).Exec()
	if err != nil {
		return fmt.Errorf("failed to delete uptime monitor: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	return tx.Commit()
}

// SaveUptimeResult saves an uptime check result
func (s *service) SaveUptimeResult(result *types.UptimeResult) error {
	query := s.sqlBuilder.
		Insert("uptime_results").
		Columns("monitor_id", "response_time_ms", "status_code", "cert_expiry", "success", "error", "created_at").
		Values(result.MonitorID, result.ResponseTimeMs, result.StatusCode, result.CertExpiry, result.Success, result.Error, result.CreatedAt)

	if s.config.Type == config.Postgres {
		query = query.Suffix("RETURNING id")
		if err := query.RunWith(s.db).QueryRow().Scan(&result.ID); err != nil {
			return fmt.Errorf("failed to save uptime result: %w", err)
		}
		return nil
	}

	res, err := query.RunWith(s.db).Exec()
	if err != nil {
		return fmt.Errorf("failed to save uptime result: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}
	result.ID = id

	return nil
}

// GetLatestUptimeResult retrieves the most recent result for a monitor
func (s *service) GetLatestUptimeResult(monitorID int64) (*types.UptimeResult, error) {
	query := s.sqlBuilder.
		Select(uptimeResultColumns...).
		From("uptime_results").
		Where(sq.Eq{"monitor_id": monitorID}).
		OrderBy("created_at DESC", "id DESC").
		Limit(1)

	result, err := scanUptimeResult(query.RunWith(s.db).QueryRow().Scan)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest uptime result: %w", err)
	}

	return result, nil
}

// GetUptimeResults retrieves paginated results for a monitor
func (s *service) GetUptimeResults(monitorID int64, page int, limit int) (*types.PaginatedUptimeResults, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 25
	}

	countQuery := s.sqlBuilder.
		Select("COUNT(*)").
		From("uptime_results").
		Where(sq.Eq{"monitor_id": monitorID})

	var total int
	if err := countQuery.RunWith(s.db).QueryRow().Scan(&total); err != nil {
		return nil, fmt.Errorf("failed to count uptime results: %w", err)
	}

	query := s.sqlBuilder.
		Select(uptimeResultColumns...).
		From("uptime_results").
		Where(sq.Eq{"monitor_id": monitorID}).
		OrderBy("created_at DESC", "id DESC").
		Limit(uint64(limit)).
		Offset(uint64((page - 1) * limit))

	rows, err := query.RunWith(s.db).Query()
	if err != nil {
		return nil, fmt.Errorf("failed to get uptime results: %w", err)
	}
	defer rows.Close()

	results := make([]types.UptimeResult, 0, limit)
	for rows.Next() {
		result, err := scanUptimeResult(rows.Scan)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan uptime result")
			continue
		}
		results = append(results, *result)
	}

	return &types.PaginatedUptimeResults{
		Data:  results,
		Total: total,
		Page:  page,
		Limit: limit,
	}, rows.Err()
}
