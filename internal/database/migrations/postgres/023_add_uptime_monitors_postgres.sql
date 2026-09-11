-- Add HTTP/TCP uptime monitoring tables

-- Table for storing uptime monitor configurations
CREATE TABLE IF NOT EXISTS uptime_monitors (
    id SERIAL PRIMARY KEY,
    name TEXT,
    type TEXT NOT NULL DEFAULT 'http',           -- http or tcp
    target TEXT NOT NULL,                        -- URL for http, host:port for tcp
    interval TEXT NOT NULL DEFAULT '60s',
    timeout_seconds INTEGER NOT NULL DEFAULT 10,
    method TEXT NOT NULL DEFAULT 'GET',          -- GET or HEAD, http only
    expected_status TEXT NOT NULL DEFAULT '2xx', -- 200, 2xx, or 200-399, http only
    keyword TEXT NOT NULL DEFAULT '',            -- body must contain this, http only
    verify_tls BOOLEAN NOT NULL DEFAULT true,
    enabled BOOLEAN DEFAULT true,
    last_run TIMESTAMP WITH TIME ZONE,
    next_run TIMESTAMP WITH TIME ZONE,
    last_state TEXT DEFAULT 'unknown',
    last_state_change TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Table for storing uptime check results
CREATE TABLE IF NOT EXISTS uptime_results (
    id SERIAL PRIMARY KEY,
    monitor_id INTEGER NOT NULL,
    response_time_ms DOUBLE PRECISION NOT NULL,
    status_code INTEGER,                         -- http only
    cert_expiry TIMESTAMP WITH TIME ZONE,        -- https only
    success BOOLEAN NOT NULL,
    error TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_uptime_results_monitor_created_at ON uptime_results(monitor_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_uptime_results_created_at ON uptime_results(created_at);

-- Uptime notification events
INSERT INTO notification_events (category, event_type, name, description, supports_threshold, threshold_unit) VALUES
('uptime', 'monitor_down', 'Uptime Monitor Down', 'HTTP or TCP target did not answer or answered wrong', false, NULL),
('uptime', 'monitor_recovered', 'Uptime Monitor Recovered', 'Previously failing HTTP or TCP target answers again', false, NULL)
ON CONFLICT DO NOTHING;
