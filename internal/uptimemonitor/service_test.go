// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package uptimemonitor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/netronome/internal/config"
	"github.com/autobrr/netronome/internal/database"
	"github.com/autobrr/netronome/internal/notifications"
	"github.com/autobrr/netronome/internal/types"
)

// testDB is one SQLite database with the schema applied. The database package
// keeps a single instance per process, so every test here shares it and works
// on its own monitor rows.
var testDB database.Service

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "netronome-uptimemonitor")
	if err != nil {
		panic(err)
	}

	testDB = database.New(config.DatabaseConfig{
		Type: config.SQLite,
		Path: filepath.Join(dir, "uptimemonitor_test.db"),
	})
	if err := testDB.InitializeTables(context.Background()); err != nil {
		panic(err)
	}

	code := m.Run()

	_ = testDB.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// startFlakyServer answers 200 while healthy is true and 503 otherwise, so a
// test can make the same monitor pass and fail.
func startFlakyServer(t *testing.T, healthy *atomic.Bool) string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func newMonitor(t *testing.T, target string, enabled bool) *types.UptimeMonitor {
	t.Helper()
	monitor, err := testDB.CreateUptimeMonitor(&types.UptimeMonitor{
		Name:           "Local service",
		Type:           types.UptimeTypeHTTP,
		Target:         target,
		Interval:       "60s",
		TimeoutSeconds: 2,
		Method:         http.MethodGet,
		ExpectedStatus: "2xx",
		VerifyTLS:      true,
		Enabled:        enabled,
	})
	require.NoError(t, err)
	return monitor
}

func TestService_RunCheck_RecordsResultAndStates(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	monitor := newMonitor(t, startFlakyServer(t, &healthy), true)

	service := NewService(testDB, nil)

	// a healthy target records a successful check and the normal state
	service.RunCheck(monitor)

	result, err := testDB.GetLatestUptimeResult(monitor.ID)
	require.NoError(t, err)
	assert.True(t, result.Success)
	require.NotNil(t, result.StatusCode)
	assert.Equal(t, 200, *result.StatusCode)
	assert.Positive(t, result.ResponseTimeMs)
	assert.Nil(t, result.Error)
	assert.Equal(t, StateOK, reloadState(t, monitor.ID))

	// a failing target moves the monitor to down and keeps the reason
	healthy.Store(false)
	service.RunCheck(monitor)

	result, err = testDB.GetLatestUptimeResult(monitor.ID)
	require.NoError(t, err)
	assert.False(t, result.Success)
	require.NotNil(t, result.StatusCode)
	assert.Equal(t, 503, *result.StatusCode)
	require.NotNil(t, result.Error)
	assert.Contains(t, *result.Error, "503")
	assert.Equal(t, StateDown, reloadState(t, monitor.ID))

	// still down: no state change
	service.RunCheck(monitor)
	assert.Equal(t, StateDown, reloadState(t, monitor.ID))

	// the next success recovers it, through a fresh service and a monitor read
	// back from the database, as a restart would do
	healthy.Store(true)
	reloaded, err := testDB.GetUptimeMonitor(monitor.ID)
	require.NoError(t, err)
	NewService(testDB, nil).RunCheck(reloaded)
	assert.Equal(t, StateRecovered, reloadState(t, monitor.ID))

	// and the success after that is plain ok again
	NewService(testDB, nil).RunCheck(reloaded)
	assert.Equal(t, StateOK, reloadState(t, monitor.ID))

	history, err := testDB.GetUptimeResults(monitor.ID, 1, 25)
	require.NoError(t, err)
	assert.Equal(t, 5, history.Total)
}

func TestService_RunCheck_BroadcastsStartAndResult(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	monitor := newMonitor(t, startFlakyServer(t, &healthy), true)

	var updates []types.UptimeUpdate
	service := NewService(testDB, nil)
	service.SetBroadcast(func(update types.UptimeUpdate) { updates = append(updates, update) })

	service.RunCheck(monitor)

	require.Len(t, updates, 2)
	assert.Equal(t, "uptime", updates[0].Type)
	assert.True(t, updates[0].IsRunning)
	assert.False(t, updates[1].IsRunning)
	assert.True(t, updates[1].Success)
	assert.Equal(t, 200, updates[1].StatusCode)
	assert.Equal(t, StateOK, updates[1].State)
}

func TestService_GetMonitorStatus_WithoutResults(t *testing.T) {
	monitor := newMonitor(t, "http://127.0.0.1:1/", false)

	status, err := NewService(testDB, nil).GetMonitorStatus(monitor.ID)
	require.NoError(t, err)
	assert.Equal(t, monitor.ID, status.MonitorID)
	assert.Equal(t, "unknown", status.State)
	assert.False(t, status.Enabled)
	assert.False(t, status.Success)
	assert.Zero(t, status.ResponseTimeMs)
	assert.Zero(t, status.StatusCode)
}

func TestService_GetMonitorStatus_Unknown(t *testing.T) {
	_, err := NewService(testDB, nil).GetMonitorStatus(99999)
	assert.ErrorIs(t, err, database.ErrNotFound)
}

func reloadState(t *testing.T, monitorID int64) string {
	t.Helper()

	monitor, err := testDB.GetUptimeMonitor(monitorID)
	require.NoError(t, err)
	return monitor.LastState
}

// TestService_Notifies checks the whole path: the seeded uptime events, a rule
// on a channel, and a down/recovered pair reaching the channel's webhook. A
// steady state sends nothing.
func TestService_Notifies(t *testing.T) {
	var mu sync.Mutex
	var received []string
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
	}))
	t.Cleanup(webhook.Close)

	channel, err := testDB.CreateChannel(database.NotificationChannelInput{
		Name: "uptime webhook",
		URL:  "generic://" + strings.TrimPrefix(webhook.URL, "http://") + "?template=json&disabletls=yes",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = testDB.DeleteChannel(channel.ID) })

	enabled := true
	for _, eventType := range []string{database.NotificationEventUptimeDown, database.NotificationEventUptimeRecovered} {
		event, err := testDB.GetEventByType(database.NotificationCategoryUptime, eventType)
		require.NoError(t, err, "migration must seed the %s event", eventType)
		_, err = testDB.CreateRule(database.NotificationRuleInput{ChannelID: channel.ID, EventID: event.ID, Enabled: &enabled})
		require.NoError(t, err)
	}

	notifier, err := notifications.NewNotifier(testDB)
	require.NoError(t, err)

	var healthy atomic.Bool
	healthy.Store(true)
	monitor := newMonitor(t, startFlakyServer(t, &healthy), true)
	monitor.Name = "Notified service"
	service := NewService(testDB, notifier)

	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(received)
	}

	service.RunCheck(monitor) // unknown -> ok: nothing to say
	assert.Equal(t, 0, count())

	healthy.Store(false)
	service.RunCheck(monitor) // ok -> down
	require.Equal(t, 1, count())
	assert.Contains(t, received[0], "[DOWN] Uptime Monitor Down")
	assert.Contains(t, received[0], "Notified service")
	assert.Contains(t, received[0], "response code 503")

	service.RunCheck(monitor) // still down: quiet
	assert.Equal(t, 1, count())

	healthy.Store(true)
	service.RunCheck(monitor) // down -> recovered
	require.Equal(t, 2, count())
	assert.Contains(t, received[1], "[OK] Uptime Monitor Recovered")

	service.RunCheck(monitor) // recovered -> ok: quiet
	assert.Equal(t, 2, count())
}
