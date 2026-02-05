//go:build cgo

package database

import _ "github.com/mattn/go-sqlite3"

// SQLiteDriver is the database/sql driver name when CGO is enabled.
const SQLiteDriver = "sqlite3"
