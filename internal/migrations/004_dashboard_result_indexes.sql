CREATE INDEX idx_results_task_queued
ON results(task_id, queued_at DESC, id DESC);

CREATE INDEX idx_results_path_queued
ON results(selected_interface, selected_source_ip, queued_at DESC, id DESC);

CREATE INDEX idx_results_status_queued
ON results(status, queued_at DESC, id DESC);
