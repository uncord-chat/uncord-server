package migrations

import "embed"

// FS embeds all SQL migration files so they are available at runtime without requiring the files on disk.
//
//go:embed *.sql
var FS embed.FS
