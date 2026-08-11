# Backup and recovery

MultiSpeed stores its SQLite database at `/data/multispeed.db` by default. In WAL mode, a live database can have committed data in `multispeed.db-wal`; copying only the main file while the process runs is not a consistent backup.

MultiSpeed can also store a managed Ookla executable at `/data/providers/ookla/speedtest` and path-isolated CLI runtime homes beneath `/data/providers/ookla/runtime`. These are separate files, not part of SQLite or a portable configuration export. An operator-mounted executable outside `/data` is also outside a complete `/data` copy and needs its own backup when redistribution and the applicable terms permit one. Process-level environment variables are deployment configuration and are never written into either backup format.

## Choose the correct backup

| Mechanism | Settings, tasks, and routes | Results and Ookla terms record | Managed Ookla files | Deployment environment |
| --- | --- | --- | --- | --- |
| Configuration JSON export | Portable active subset | No | No | No |
| Online SQLite backup | Yes | Yes | No | No |
| Stopped copy of complete `/data` | Yes | Yes | Executable and CLI runtime homes | No |

Back up the container definition or environment-variable configuration separately. Values such as listener policy, custom-server allowlists, the Ookla upload opt-in, and a headless EULA override are intentionally deployment-owned and non-portable.

## Online backup

Use the application endpoint while MultiSpeed is healthy:

```bash
curl --fail-with-body \
  --request POST \
  --output "multispeed-$(date -u +%Y%m%dT%H%M%SZ).db" \
  http://127.0.0.1:8787/api/v1/backup
```

The endpoint uses SQLite's consistency mechanism and returns a complete database snapshot. It does not include `/data/providers/ookla/speedtest`, `/data/providers/ookla/runtime`, or any other non-database file. Store it encrypted with access controls appropriate for source addresses, public IP history, task descriptions, provider targets, and diagnostics.

Periodically restore a backup into a disposable environment and check readiness; an unread backup is not a recovery plan.

## Portable configuration export

The JSON export in **Settings → Configuration import & export** is useful for transferring operational settings, active tasks, and active route profiles between MultiSpeed installations. It deliberately excludes measurement results, provider diagnostics, soft-deleted definitions, route-validation observations, schedule runtime timestamps, the legacy-named Ookla terms acknowledgement state, the managed Ookla executable, and every process environment variable.

Import validates the complete versioned document and replaces the portable configuration atomically. Existing result rows and their task/profile identities remain in the database, and the Ookla terms acknowledgement/CLI flag authorization remains a separate operator decision on the destination. Enabled tasks must refer to a source address currently present on the configured interface; disable or edit non-portable tasks before transferring them to a different network layout.

A configuration export is not a disaster-recovery backup. Keep verified SQLite backups for full recovery.

## Offline filesystem backup

Stop the service before copying the bind-mounted directory:

```bash
docker compose stop multispeed
cp -a data "data-backup-$(date -u +%Y%m%dT%H%M%SZ)"
docker compose start multispeed
```

Preserve the directory as a unit, including any `multispeed.db-wal` and `multispeed.db-shm` files and the `providers/` subtree. Do not edit or merge SQLite files manually. Record the numeric UID/GID used by the container so ownership can be restored; the published image defaults to `10001:10001`, while the supplied Compose file may override it.

## Restore

1. Stop the container.
2. Preserve the current `data/` directory separately.
3. Place the verified backup at `data/multispeed.db` with no unrelated WAL/SHM sidecars from another database generation.
4. Set ownership to the non-root `MULTISPEED_UID:MULTISPEED_GID` used by Compose and restrict file permissions.
5. Start the same application version that created the backup first.
6. Check logs, `/api/v1/healthz`, `/api/v1/readyz`, schema version, task count, and recent results.
7. Upgrade deliberately after the restored version is healthy.

If the source deployment used Ookla, confirm that `/data/providers/ookla/speedtest` was restored as a regular executable file when managed there and that both its parent directory and `/data/providers/ookla/runtime` are writable by the runtime UID. Restore environment policy separately; in particular, managed upload remains disabled unless `APP_ALLOW_OOKLA_BINARY_UPLOAD=true` is present in the destination deployment.

Never restore over a running database. Never downgrade an already migrated database unless a release explicitly documents that path.

## Integrity trouble

If readiness reports corruption or a migration failure:

- stop writes immediately and preserve the entire data directory;
- save the exact image tag, logs, database/WAL/SHM files, and filesystem/storage error information;
- attempt recovery only on copies;
- prefer restoring the newest verified online backup;
- do not run destructive SQLite repair commands without understanding the data-loss boundary.

Sanitize public IPs, source addresses, route metadata, and provider diagnostics before sharing artifacts in a public issue. Use the private security-reporting path when exposure could be sensitive.

Configuration exports can additionally contain interface names, task descriptions, route notes, target identifiers, and custom backend URLs. Review those fields before sharing an export even though measurement history is excluded. See [Privacy and data flow](privacy.md).
