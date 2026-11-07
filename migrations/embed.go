// Package migrations ships the SQL schema inside the binary.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
