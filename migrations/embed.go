// Package migrations embeds the SQL migration files so the API binary can
// run them on boot without needing the migrations/ directory on disk.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
