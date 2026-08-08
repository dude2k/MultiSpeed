# Backup and recovery

MultiSpeed stores its SQLite database at `/data/multispeed.db` by default. In WAL mode, a live database can have committed data in `multispeed.db-wal`; copying only the main file while the process runs is not a consistent backup.

## Online backup

Use the application endpoint while MultiSpeed is healthy:

```bash
curl --fail-with-body \
  --request POST \
  --output "multispeed-$(date -u +%Y%m%dT%H%M%SZ).db" \
  http://127.0.0.1:8787/api/v1/backup
```

The endpoint uses SQLite's consistency mechanism and returns a complete database snapshot. Store it encrypted with access controls appropriate for source addresses, public IP history, task descriptions, provider targets, and diagnostics.

Periodically restore a backup into a disposable environment and check readiness; an unread backup is not a recovery plan.

## Portable configuration export

The JSON export in **Settings → Configuration import & export** is useful for transferring operational settings, active tasks, and active route profiles between MultiSpeed installations. It deliberately excludes measurement results, provider diagnostics, soft-deleted definitions, route-validation observations, schedule runtime timestamps, and Ookla EULA acknowledgement state.

Import validates the complete versioned document and replaces the portable configuration atomically. Existing result rows and their task/profile identities remain in the database, and EULA acceptance remains a separate operator decision on the destination. Enabled tasks must refer to a source address currently present on the configured interface; disable or edit non-portable tasks before transferring them to a different network layout.

A configuration export is not a disaster-recovery backup. Keep verified SQLite backups for full recovery.

## Offline filesystem backup

Stop the service before copying the bind-mounted directory:

```bash
docker compose stop multispeed
cp -a data "data-backup-$(date -u +%Y%m%dT%H%M%SZ)"
docker compose start multispeed
```

Preserve the directory as a unit, including any `multispeed.db-wal` and `multispeed.db-shm` files. Do not edit or merge SQLite files manually.

## Restore

1. Stop the container.
2. Preserve the current `data/` directory separately.
3. Place the verified backup at `data/multispeed.db` with no unrelated WAL/SHM sidecars from another database generation.
4. Set ownership to the non-root `MULTISPEED_UID:MULTISPEED_GID` used by Compose and restrict file permissions.
5. Start the same application version that created the backup first.
6. Check logs, `/api/v1/healthz`, `/api/v1/readyz`, schema version, task count, and recent results.
7. Upgrade deliberately after the restored version is healthy.

Never restore over a running database. Never downgrade an already migrated database unless a release explicitly documents that path.

## Integrity trouble

If readiness reports corruption or a migration failure:

- stop writes immediately and preserve the entire data directory;
- save the exact image tag, logs, database/WAL/SHM files, and filesystem/storage error information;
- attempt recovery only on copies;
- prefer restoring the newest verified online backup;
- do not run destructive SQLite repair commands without understanding the data-loss boundary.

Sanitize public IPs, source addresses, route metadata, and provider diagnostics before sharing artifacts in a public issue. Use the private security-reporting path when exposure could be sensitive.
