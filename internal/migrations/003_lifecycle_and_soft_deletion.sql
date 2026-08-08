ALTER TABLE tasks ADD COLUMN deleted_at TEXT;
ALTER TABLE route_profiles ADD COLUMN deleted_at TEXT;
ALTER TABLE results ADD COLUMN queued_at TEXT;

UPDATE results
SET queued_at = COALESCE(scheduled_at, started_at, finished_at,
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE queued_at IS NULL;

-- Timestamps are compared as TEXT, so normalize existing values to the same
-- fixed-width UTC representation used by the application.
UPDATE tasks SET
    created_at = strftime('%Y-%m-%dT%H:%M:', created_at) || substr(strftime('%f', created_at), 1, 6) || '000000Z',
    updated_at = strftime('%Y-%m-%dT%H:%M:', updated_at) || substr(strftime('%f', updated_at), 1, 6) || '000000Z',
    last_scheduled_at = CASE WHEN last_scheduled_at IS NULL THEN NULL ELSE strftime('%Y-%m-%dT%H:%M:', last_scheduled_at) || substr(strftime('%f', last_scheduled_at), 1, 6) || '000000Z' END,
    next_scheduled_at = CASE WHEN next_scheduled_at IS NULL THEN NULL ELSE strftime('%Y-%m-%dT%H:%M:', next_scheduled_at) || substr(strftime('%f', next_scheduled_at), 1, 6) || '000000Z' END;

UPDATE route_profiles SET
    created_at = strftime('%Y-%m-%dT%H:%M:', created_at) || substr(strftime('%f', created_at), 1, 6) || '000000Z',
    updated_at = strftime('%Y-%m-%dT%H:%M:', updated_at) || substr(strftime('%f', updated_at), 1, 6) || '000000Z',
    last_validation_at = CASE WHEN last_validation_at IS NULL THEN NULL ELSE strftime('%Y-%m-%dT%H:%M:', last_validation_at) || substr(strftime('%f', last_validation_at), 1, 6) || '000000Z' END;

UPDATE results SET
    queued_at = strftime('%Y-%m-%dT%H:%M:', queued_at) || substr(strftime('%f', queued_at), 1, 6) || '000000Z',
    scheduled_at = CASE WHEN scheduled_at IS NULL THEN NULL ELSE strftime('%Y-%m-%dT%H:%M:', scheduled_at) || substr(strftime('%f', scheduled_at), 1, 6) || '000000Z' END,
    started_at = CASE WHEN started_at IS NULL THEN NULL ELSE strftime('%Y-%m-%dT%H:%M:', started_at) || substr(strftime('%f', started_at), 1, 6) || '000000Z' END,
    finished_at = CASE WHEN finished_at IS NULL THEN NULL ELSE strftime('%Y-%m-%dT%H:%M:', finished_at) || substr(strftime('%f', finished_at), 1, 6) || '000000Z' END;

CREATE INDEX idx_tasks_live ON tasks(deleted_at, name);
CREATE INDEX idx_route_profiles_live ON route_profiles(deleted_at, name);
CREATE INDEX idx_results_queued ON results(queued_at DESC);

CREATE TRIGGER validate_result_metrics_insert
BEFORE INSERT ON results
WHEN (NEW.download_bps IS NOT NULL AND NEW.download_bps < 0)
  OR (NEW.upload_bps IS NOT NULL AND NEW.upload_bps < 0)
  OR (NEW.latency_ms IS NOT NULL AND NEW.latency_ms < 0)
  OR (NEW.jitter_ms IS NOT NULL AND NEW.jitter_ms < 0)
  OR (NEW.packet_loss_percent IS NOT NULL AND (NEW.packet_loss_percent < 0 OR NEW.packet_loss_percent > 100))
  OR (NEW.download_bytes IS NOT NULL AND NEW.download_bytes < 0)
  OR (NEW.upload_bytes IS NOT NULL AND NEW.upload_bytes < 0)
  OR NEW.execution_duration_ms < 0
BEGIN
  SELECT RAISE(ABORT, 'invalid result metrics');
END;

CREATE TRIGGER validate_result_metrics_update
BEFORE UPDATE OF download_bps, upload_bps, latency_ms, jitter_ms, packet_loss_percent,
  download_bytes, upload_bytes, execution_duration_ms ON results
WHEN (NEW.download_bps IS NOT NULL AND NEW.download_bps < 0)
  OR (NEW.upload_bps IS NOT NULL AND NEW.upload_bps < 0)
  OR (NEW.latency_ms IS NOT NULL AND NEW.latency_ms < 0)
  OR (NEW.jitter_ms IS NOT NULL AND NEW.jitter_ms < 0)
  OR (NEW.packet_loss_percent IS NOT NULL AND (NEW.packet_loss_percent < 0 OR NEW.packet_loss_percent > 100))
  OR (NEW.download_bytes IS NOT NULL AND NEW.download_bytes < 0)
  OR (NEW.upload_bytes IS NOT NULL AND NEW.upload_bytes < 0)
  OR NEW.execution_duration_ms < 0
BEGIN
  SELECT RAISE(ABORT, 'invalid result metrics');
END;
