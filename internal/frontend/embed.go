// Package frontend exposes the production Vite bundle to the Go server.
package frontend

import "embed"

// Files contains the Vite output synchronized by web/npm run build.
//
//go:embed dist/*
var Files embed.FS
