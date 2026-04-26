// Package migrations holds the SQL migration files embedded at build time.
package migrations

import "embed"

// FS is the embed.FS containing all *.sql migration files.
//
//go:embed *.sql
var FS embed.FS
