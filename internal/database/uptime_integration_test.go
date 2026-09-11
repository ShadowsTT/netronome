// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/netronome/internal/types"
)

func TestUptimeMonitor_CRUD(t *testing.T) {
	RunTestWithBothDatabases(t, func(t *testing.T, td *TestDatabase) {
		created, err := td.Service.CreateUptimeMonitor(&types.UptimeMonitor{
			Name:           "Website",
			Type:           types.UptimeTypeHTTP,
			Target:         "https://example.invalid/health",
			Interval:       "60s",
			TimeoutSeconds: 10,
			Method:         "GET",
			ExpectedStatus: "2xx",
			Keyword:        "ok",
			VerifyTLS:      true,
			Enabled:        true,
		})
		require.NoError(t, err)
		assert.Greater(t, created.ID, int64(0))

		retrieved, err := td.Service.GetUptimeMonitor(created.ID)
		require.NoError(t, err)
		assert.Equal(t, "Website", retrieved.Name)
		assert.Equal(t, types.UptimeTypeHTTP, retrieved.Type)
		assert.Equal(t, "https://example.invalid/health", retrieved.Target)
		assert.Equal(t, 10, retrieved.TimeoutSeconds)
		assert.Equal(t, "GET", retrieved.Method)
		assert.Equal(t, "2xx", retrieved.ExpectedStatus)
		assert.Equal(t, "ok", retrieved.Keyword)
		assert.True(t, retrieved.VerifyTLS)
		assert.True(t, retrieved.Enabled)

		retrieved.Name = "SSH"
		retrieved.Type = types.UptimeTypeTCP
		retrieved.Target = "192.168.1.1:22"
		retrieved.VerifyTLS = false
		retrieved.Enabled = false
		require.NoError(t, td.Service.UpdateUptimeMonitor(retrieved))

		updated, err := td.Service.GetUptimeMonitor(created.ID)
		require.NoError(t, err)
		assert.Equal(t, "SSH", updated.Name)
		assert.Equal(t, types.UptimeTypeTCP, updated.Type)
		assert.Equal(t, "192.168.1.1:22", updated.Target)
		assert.False(t, updated.VerifyTLS)
		assert.False(t, updated.Enabled)

		monitors, err := td.Service.GetUptimeMonitors()
		require.NoError(t, err)
		assert.Len(t, monitors, 1)

		require.NoError(t, td.Service.DeleteUptimeMonitor(created.ID))

		_, err = td.Service.GetUptimeMonitor(created.ID)
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestUptimeMonitor_NotFound(t *testing.T) {
	RunTestWithBothDatabases(t, func(t *testing.T, td *TestDatabase) {
		_, err := td.Service.GetUptimeMonitor(99999)
		assert.ErrorIs(t, err, ErrNotFound)

		assert.ErrorIs(t, td.Service.UpdateUptimeMonitorState(99999, "down"), ErrNotFound)
		assert.ErrorIs(t, td.Service.UpdateUptimeMonitorSchedule(99999, nil, time.Now()), ErrNotFound)
		assert.ErrorIs(t, td.Service.DeleteUptimeMonitor(99999), ErrNotFound)
		_, err = td.Service.GetLatestUptimeResult(99999)
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestUpdateUptimeMonitorState(t *testing.T) {
	RunTestWithBothDatabases(t, func(t *testing.T, td *TestDatabase) {
		monitor := CreateTestUptimeMonitor(t, td)

		require.NoError(t, td.Service.UpdateUptimeMonitorState(monitor.ID, "down"))

		updated, err := td.Service.GetUptimeMonitor(monitor.ID)
		require.NoError(t, err)
		assert.Equal(t, "down", updated.LastState)
		assert.NotNil(t, updated.LastStateChange)
	})
}

func TestUpdateUptimeMonitorSchedule_KeepsConfiguration(t *testing.T) {
	RunTestWithBothDatabases(t, func(t *testing.T, td *TestDatabase) {
		monitor := CreateTestUptimeMonitor(t, td)

		// an edit lands while the scheduler holds its stale copy
		monitor.Target = "http://127.0.0.1:9/edited"
		require.NoError(t, td.Service.UpdateUptimeMonitor(monitor))

		lastRun := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
		nextRun := time.Now().UTC().Truncate(time.Second)
		require.NoError(t, td.Service.UpdateUptimeMonitorSchedule(monitor.ID, &lastRun, nextRun))

		reloaded, err := td.Service.GetUptimeMonitor(monitor.ID)
		require.NoError(t, err)
		assert.Equal(t, "http://127.0.0.1:9/edited", reloaded.Target)
		require.NotNil(t, reloaded.LastRun)
		assert.WithinDuration(t, lastRun, *reloaded.LastRun, time.Second)
		require.NotNil(t, reloaded.NextRun)
		assert.WithinDuration(t, nextRun, *reloaded.NextRun, time.Second)
	})
}

func TestUptimeResults_LatestAndPagination(t *testing.T) {
	RunTestWithBothDatabases(t, func(t *testing.T, td *TestDatabase) {
		monitor := CreateTestUptimeMonitor(t, td)

		base := time.Now().Add(-time.Hour)
		for i := 0; i < 5; i++ {
			code := 200
			var errText *string
			if i == 4 {
				code = 503
				text := "response code 503"
				errText = &text
			}
			expiry := base.Add(30 * 24 * time.Hour)
			require.NoError(t, td.Service.SaveUptimeResult(&types.UptimeResult{
				MonitorID:      monitor.ID,
				ResponseTimeMs: float64(10 + i),
				StatusCode:     &code,
				CertExpiry:     &expiry,
				Success:        i != 4,
				Error:          errText,
				CreatedAt:      base.Add(time.Duration(i) * time.Minute),
			}))
		}

		latest, err := td.Service.GetLatestUptimeResult(monitor.ID)
		require.NoError(t, err)
		assert.False(t, latest.Success)
		require.NotNil(t, latest.StatusCode)
		assert.Equal(t, 503, *latest.StatusCode)
		require.NotNil(t, latest.CertExpiry)
		require.NotNil(t, latest.Error)
		assert.Equal(t, float64(14), latest.ResponseTimeMs)

		page1, err := td.Service.GetUptimeResults(monitor.ID, 1, 2)
		require.NoError(t, err)
		assert.Equal(t, 5, page1.Total)
		assert.Len(t, page1.Data, 2)
		assert.Equal(t, float64(14), page1.Data[0].ResponseTimeMs)
		assert.Equal(t, float64(13), page1.Data[1].ResponseTimeMs)

		page3, err := td.Service.GetUptimeResults(monitor.ID, 3, 2)
		require.NoError(t, err)
		assert.Len(t, page3.Data, 1)
		assert.Equal(t, float64(10), page3.Data[0].ResponseTimeMs)

		// removing the monitor takes its results with it
		require.NoError(t, td.Service.DeleteUptimeMonitor(monitor.ID))
		AssertRecordNotExists(t, td, "uptime_results", "monitor_id", monitor.ID)
	})
}
