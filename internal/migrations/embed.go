// Package migrations exposes the ordered embedded SQLite migrations.
package migrations

import "embed"

// Files contains all schema migrations.
//
//go:embed *.sql
var Files embed.FS
