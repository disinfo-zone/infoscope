//go:build !cgo

package database

import _ "modernc.org/sqlite"

// SQLiteDriver is the database/sql driver name when CGO is disabled.
const SQLiteDriver = "sqlite"
