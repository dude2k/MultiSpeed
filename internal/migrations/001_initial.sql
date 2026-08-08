CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    provider TEXT NOT NULL CHECK(provider IN ('ookla', 'librespeed', 'cloudflare')),
    cron_expression TEXT NOT NULL,
    timezone TEXT NOT NULL,
    random_jitter_seconds INTEGER NOT NULL DEFAULT 0 CHECK(random_jitter_seconds BETWEEN 0 AND 3600),
    server_selection_mode TEXT NOT NULL DEFAULT 'automatic',
    server_id TEXT NOT NULL DEFAULT '',
    server_url TEXT NOT NULL DEFAULT '',
    custom_server_definition TEXT NOT NULL DEFAULT '{}',
    interface_name TEXT NOT NULL,
    source_ip TEXT NOT NULL,
    ip_family TEXT NOT NULL DEFAULT 'auto' CHECK(ip_family IN ('auto', 'ipv4', 'ipv6')),
    route_profile_id TEXT REFERENCES route_profiles(id) ON DELETE SET NULL,
    timeout_seconds INTEGER NOT NULL DEFAULT 120 CHECK(timeout_seconds BETWEEN 5 AND 3600),
    provider_options TEXT NOT NULL DEFAULT '{}',
    prevent_overlap INTEGER NOT NULL DEFAULT 1 CHECK(prevent_overlap IN (0, 1)),
    route_validation TEXT NOT NULL DEFAULT 'required' CHECK(route_validation IN ('required', 'interface-only')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_scheduled_at TEXT,
    next_scheduled_at TEXT
);

CREATE TABLE route_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT '',
    interface_name TEXT NOT NULL,
    source_ip TEXT NOT NULL,
    expected_gateway TEXT NOT NULL DEFAULT '',
    expected_routing_table TEXT NOT NULL DEFAULT '',
    validation_target TEXT NOT NULL DEFAULT '1.1.1.1',
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_validation_at TEXT,
    last_validation_succeeded INTEGER,
    last_validation_snapshot TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE results (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    route_profile_id TEXT REFERENCES route_profiles(id) ON DELETE SET NULL,
    trigger_type TEXT NOT NULL CHECK(trigger_type IN ('scheduled', 'manual')),
    provider TEXT NOT NULL,
    scheduled_at TEXT,
    started_at TEXT,
    finished_at TEXT,
    status TEXT NOT NULL CHECK(status IN ('queued', 'validating', 'running', 'succeeded', 'failed', 'skipped', 'cancelled')),
    download_bps INTEGER,
    upload_bps INTEGER,
    latency_ms REAL,
    jitter_ms REAL,
    packet_loss_percent REAL,
    download_bytes INTEGER,
    upload_bytes INTEGER,
    selected_interface TEXT NOT NULL,
    selected_source_ip TEXT NOT NULL,
    detected_public_ip TEXT NOT NULL DEFAULT '',
    server_id TEXT NOT NULL DEFAULT '',
    server_name TEXT NOT NULL DEFAULT '',
    server_host TEXT NOT NULL DEFAULT '',
    server_sponsor TEXT NOT NULL DEFAULT '',
    server_location TEXT NOT NULL DEFAULT '',
    server_country TEXT NOT NULL DEFAULT '',
    provider_result_url TEXT NOT NULL DEFAULT '',
    cloudflare_colo TEXT NOT NULL DEFAULT '',
    route_validation_snapshot TEXT NOT NULL DEFAULT '{}',
    execution_duration_ms INTEGER NOT NULL DEFAULT 0,
    process_exit_code INTEGER,
    sanitized_error TEXT NOT NULL DEFAULT '',
    raw_provider_response TEXT NOT NULL DEFAULT '',
    provider_version TEXT NOT NULL DEFAULT '',
    application_version TEXT NOT NULL DEFAULT ''
);

CREATE TABLE settings (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    display_units TEXT NOT NULL DEFAULT 'bits',
    default_timezone TEXT NOT NULL DEFAULT 'UTC',
    global_concurrency INTEGER NOT NULL DEFAULT 1 CHECK(global_concurrency BETWEEN 1 AND 16),
    allow_separate_wan_concurrency INTEGER NOT NULL DEFAULT 0 CHECK(allow_separate_wan_concurrency IN (0, 1)),
    retention_mode TEXT NOT NULL DEFAULT 'forever' CHECK(retention_mode IN ('forever', 'days', 'months')),
    retention_value INTEGER NOT NULL DEFAULT 0 CHECK(retention_value >= 0),
    default_chart_range TEXT NOT NULL DEFAULT '30d',
    interface_refresh_interval_seconds INTEGER NOT NULL DEFAULT 30 CHECK(interface_refresh_interval_seconds BETWEEN 5 AND 3600),
    default_task_timeout_seconds INTEGER NOT NULL DEFAULT 120 CHECK(default_task_timeout_seconds BETWEEN 5 AND 3600),
    database_maintenance_schedule TEXT NOT NULL DEFAULT '0 3 * * 0'
);

INSERT INTO settings(singleton) VALUES (1);

CREATE INDEX idx_tasks_enabled_next ON tasks(enabled, next_scheduled_at);
CREATE INDEX idx_results_task_started ON results(task_id, started_at DESC);
CREATE INDEX idx_results_started ON results(started_at DESC);
CREATE INDEX idx_results_finished ON results(finished_at DESC);
CREATE INDEX idx_results_status ON results(status);
CREATE INDEX idx_results_provider ON results(provider);
CREATE INDEX idx_results_interface ON results(selected_interface);
CREATE INDEX idx_results_source_ip ON results(selected_source_ip);
CREATE INDEX idx_results_server_id ON results(server_id);
CREATE INDEX idx_results_grouping ON results(status, provider, started_at);

