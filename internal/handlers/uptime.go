// Copyright (c) 2024-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/netronome/internal/database"
	"github.com/autobrr/netronome/internal/scheduler"
	"github.com/autobrr/netronome/internal/types"
	"github.com/autobrr/netronome/internal/uptimemonitor"
)

// UptimeHandler handles HTTP/TCP uptime monitoring endpoints
type UptimeHandler struct {
	db        database.Service
	service   *uptimemonitor.Service
	scheduler scheduler.Service
}

func NewUptimeHandler(db database.Service, service *uptimemonitor.Service, scheduler scheduler.Service) *UptimeHandler {
	return &UptimeHandler{db: db, service: service, scheduler: scheduler}
}

// uptimeMonitorRequest is the monitor as the API takes it. Enabled and
// VerifyTLS are pointers so that a body without the field keeps the default
// instead of the zero value.
type uptimeMonitorRequest struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Target         string `json:"target"`
	Interval       string `json:"interval"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Method         string `json:"method"`
	ExpectedStatus string `json:"expectedStatus"`
	Keyword        string `json:"keyword"`
	VerifyTLS      *bool  `json:"verifyTls"`
	Enabled        *bool  `json:"enabled"`
}

// monitor applies the request to a monitor, with enabled and verifyTLS as the
// fallbacks for a request that does not carry the fields.
func (r uptimeMonitorRequest) monitor(enabled, verifyTLS bool) types.UptimeMonitor {
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	if r.VerifyTLS != nil {
		verifyTLS = *r.VerifyTLS
	}

	return types.UptimeMonitor{
		Name:           r.Name,
		Type:           r.Type,
		Target:         r.Target,
		Interval:       r.Interval,
		TimeoutSeconds: r.TimeoutSeconds,
		Method:         r.Method,
		ExpectedStatus: r.ExpectedStatus,
		Keyword:        r.Keyword,
		VerifyTLS:      verifyTLS,
		Enabled:        enabled,
	}
}

// normalizeUptimeMonitor fills in the defaults and reports the first invalid
// field. Its error text goes straight to the caller.
func normalizeUptimeMonitor(monitor *types.UptimeMonitor) error {
	monitor.Name = strings.TrimSpace(monitor.Name)

	monitor.Type = strings.ToLower(strings.TrimSpace(monitor.Type))
	if monitor.Type == "" {
		monitor.Type = types.UptimeTypeHTTP
	}

	monitor.Target = strings.TrimSpace(monitor.Target)
	if monitor.Target == "" {
		return errors.New("Target is required")
	}

	switch monitor.Type {
	case types.UptimeTypeHTTP:
		if err := normalizeHTTPFields(monitor); err != nil {
			return err
		}
	case types.UptimeTypeTCP:
		if err := validateHostPort(monitor.Target); err != nil {
			return err
		}
		// the http fields do not apply; keep the column defaults
		monitor.Method = http.MethodGet
		monitor.ExpectedStatus = "2xx"
		monitor.Keyword = ""
	default:
		return errors.New("Type must be one of: http, tcp")
	}

	if monitor.TimeoutSeconds == 0 {
		monitor.TimeoutSeconds = uptimemonitor.DefaultTimeout
	}
	if monitor.TimeoutSeconds < uptimemonitor.MinTimeoutSeconds || monitor.TimeoutSeconds > uptimemonitor.MaxTimeoutSeconds {
		return fmt.Errorf("Timeout must be between %d and %d seconds", uptimemonitor.MinTimeoutSeconds, uptimemonitor.MaxTimeoutSeconds)
	}

	if strings.TrimSpace(monitor.Interval) == "" {
		monitor.Interval = "60s"
	}

	return nil
}

func normalizeHTTPFields(monitor *types.UptimeMonitor) error {
	target, err := url.Parse(monitor.Target)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return errors.New("Target must be an http:// or https:// URL")
	}

	monitor.Method = strings.ToUpper(strings.TrimSpace(monitor.Method))
	if monitor.Method == "" {
		monitor.Method = http.MethodGet
	}
	if !slices.Contains(uptimemonitor.Methods, monitor.Method) {
		return errors.New("Method must be one of: " + strings.Join(uptimemonitor.Methods, ", "))
	}

	monitor.ExpectedStatus = strings.ToLower(strings.TrimSpace(monitor.ExpectedStatus))
	if monitor.ExpectedStatus == "" {
		monitor.ExpectedStatus = "2xx"
	}
	if _, err := uptimemonitor.ParseExpectedStatus(monitor.ExpectedStatus); err != nil {
		return errors.New("Expected status must be a code (200), a class (2xx), or a range (200-399)")
	}

	monitor.Keyword = strings.TrimSpace(monitor.Keyword)
	if monitor.Keyword != "" && monitor.Method == http.MethodHead {
		return errors.New("Keyword needs the GET method, HEAD has no body")
	}

	return nil
}

func validateHostPort(target string) error {
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" {
		return errors.New("Target must be host:port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("Port must be between 1 and 65535")
	}
	return nil
}

// GetMonitors returns all uptime monitors
func (h *UptimeHandler) GetMonitors(c *gin.Context) {
	monitors, err := h.db.GetUptimeMonitors()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get uptime monitors")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get monitors"})
		return
	}

	c.JSON(http.StatusOK, monitors)
}

// CreateMonitor creates a new uptime monitor
func (h *UptimeHandler) CreateMonitor(c *gin.Context) {
	var request uptimeMonitorRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// a new monitor runs and verifies certificates unless the caller says
	// otherwise
	monitor := request.monitor(true, true)
	if err := normalizeUptimeMonitor(&monitor); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// a new monitor runs on the next scheduler tick; the interval only has to
	// parse
	now := time.Now().UTC()
	if h.scheduler.CalculateNextRun(monitor.Interval, now).IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid interval"})
		return
	}
	monitor.NextRun = &now

	created, err := h.db.CreateUptimeMonitor(&monitor)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create uptime monitor")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create monitor"})
		return
	}

	c.JSON(http.StatusCreated, created)
}

// UpdateMonitor updates an existing uptime monitor
func (h *UptimeHandler) UpdateMonitor(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid monitor ID"})
		return
	}

	var request uptimeMonitorRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// keep the scheduling fields the user does not send
	monitor, err := h.db.GetUptimeMonitor(id)
	if err != nil {
		if err == database.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Monitor not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get monitor"})
		return
	}

	updateData := request.monitor(monitor.Enabled, monitor.VerifyTLS)
	if err := normalizeUptimeMonitor(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	monitor.Name = updateData.Name
	monitor.Type = updateData.Type
	monitor.Target = updateData.Target
	monitor.TimeoutSeconds = updateData.TimeoutSeconds
	monitor.Method = updateData.Method
	monitor.ExpectedStatus = updateData.ExpectedStatus
	monitor.Keyword = updateData.Keyword
	monitor.VerifyTLS = updateData.VerifyTLS
	monitor.Enabled = updateData.Enabled

	// same as CreateMonitor: a new interval takes effect on the next tick
	if monitor.Interval != updateData.Interval {
		monitor.Interval = updateData.Interval
		now := time.Now().UTC()
		if h.scheduler.CalculateNextRun(monitor.Interval, now).IsZero() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid interval"})
			return
		}
		monitor.NextRun = &now
	}

	if err := h.db.UpdateUptimeMonitor(monitor); err != nil {
		log.Error().Err(err).Int64("monitorID", id).Msg("Failed to update uptime monitor")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update monitor"})
		return
	}

	c.JSON(http.StatusOK, monitor)
}

// DeleteMonitor removes an uptime monitor and its results
func (h *UptimeHandler) DeleteMonitor(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid monitor ID"})
		return
	}

	if err := h.db.DeleteUptimeMonitor(id); err != nil {
		if err == database.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Monitor not found"})
			return
		}
		log.Error().Err(err).Int64("monitorID", id).Msg("Failed to delete uptime monitor")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete monitor"})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// GetMonitorStatus returns the last known status of a monitor
func (h *UptimeHandler) GetMonitorStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid monitor ID"})
		return
	}

	status, err := h.service.GetMonitorStatus(id)
	if err != nil {
		if err == database.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Monitor not found"})
			return
		}
		log.Error().Err(err).Int64("monitorID", id).Msg("Failed to get uptime monitor status")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get monitor status"})
		return
	}

	c.JSON(http.StatusOK, status)
}

// GetMonitorHistory returns paginated results for a monitor
func (h *UptimeHandler) GetMonitorHistory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid monitor ID"})
		return
	}

	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page <= 0 {
		page = 1
	}

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "25"))
	if err != nil || limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	// an unknown monitor is a 404 here as it is on the status route, not an
	// empty page
	if _, err := h.db.GetUptimeMonitor(id); err != nil {
		if err == database.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Monitor not found"})
			return
		}
		log.Error().Err(err).Int64("monitorID", id).Msg("Failed to get uptime monitor")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get monitor history"})
		return
	}

	results, err := h.db.GetUptimeResults(id, page, limit)
	if err != nil {
		log.Error().Err(err).Int64("monitorID", id).Msg("Failed to get uptime results")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get monitor history"})
		return
	}

	c.JSON(http.StatusOK, results)
}
