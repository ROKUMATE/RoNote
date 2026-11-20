// Package migrations holds the goose migrations, embedded so the api binary
// can apply them and the api and worker can check a database against them
// (internal/schema).
package migrations

import "embed"

// FS is every migration, by file name.
//
//go:embed *.sql
var FS embed.FS
