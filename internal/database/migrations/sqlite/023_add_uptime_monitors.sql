-- Add HTTP/TCP uptime monitoring tables

-- Table for storing uptime monitor configurations
CREATE TABLE uptime_monitors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT,
    type TEXT NOT NULL DEFAULT 'http',           -- http or tcp
    target TEXT NOT NULL,                        -- URL for http, host:port for tcp
    interval TEXT NOT NULL DEFAULT '60s',
    timeout_seconds INTEGER NOT NULL DEFAULT 10,
    method TEXT NOT NULL DEFAULT 'GET',          -- GET or HEAD, http only
    expected_status TEXT NOT NULL DEFAULT '2xx', -- 200, 2xx, or 200-399, http only
    keyword TEXT NOT NULL DEFAULT '',            -- body must contain this, http only
    verify_tls BOOLEAN NOT NULL DEFAULT 1,
    enabled BOOLEAN DEFAULT 1,
    last_run TIMESTAMP,
    next_run TIMESTAMP,
    last_state TEXT DEFAULT 'unknown',
    last_state_change TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Table for storing uptime check results
CREATE TABLE uptime_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id INTEGER NOT NULL,
    response_time_ms REAL NOT NULL,
    status_code INTEGER,                         -- http only
    cert_expiry TIMESTAMP,                       -- https only
    success BOOLEAN NOT NULL,
    error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
);

CREATE INDEX idx_uptime_results_monitor_created_at ON uptime_results(monitor_id, created_at DESC, id DESC);
CREATE INDEX idx_uptime_results_created_at ON uptime_results(created_at);

-- Uptime notification events
INSERT INTO notification_events (category, event_type, name, description, supports_threshold, threshold_unit) VALUES
('uptime', 'monitor_down', 'Uptime Monitor Down', 'HTTP or TCP target did not answer or answered wrong', 0, NULL),
('uptime', 'monitor_recovered', 'Uptime Monitor Recovered', 'Previously failing HTTP or TCP target answers again', 0, NULL);
