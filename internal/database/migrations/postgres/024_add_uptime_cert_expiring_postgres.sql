-- Uptime certificate expiry notification event
INSERT INTO notification_events (category, event_type, name, description, supports_threshold, threshold_unit) VALUES
('uptime', 'cert_expiring', 'Certificate Expiring', 'Days until the HTTPS certificate expires (use a "less than" operator)', true, 'days')
ON CONFLICT DO NOTHING;
