package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
)

// SidecarDir returns a sibling directory next to the configured SQLite file.
// For in-memory DSNs, it returns an empty string.
func (db *DB) SidecarDir(name string) string {
	if db == nil {
		return ""
	}

	return sidecarDirFromLocation(db.cfg.DSN, name)
}

// SidecarDirForSQLDB returns a sibling directory next to the main database file
// for a generic *sql.DB handle. For in-memory databases or non-SQLite handles,
// it returns an empty string.
func SidecarDirForSQLDB(ctx context.Context, db *sql.DB, name string) string {
	if db == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}

	rows, err := db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return ""
	}
	defer rows.Close()

	for rows.Next() {
		var seq int
		var schema, file string
		if err := rows.Scan(&seq, &schema, &file); err != nil {
			continue
		}
		if schema != "main" {
			continue
		}
		return sidecarDirFromLocation(file, name)
	}

	return ""
}

func sidecarDirFromLocation(dsn string, name string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" || dsn == ":memory:" || strings.Contains(dsn, "mode=memory") {
		return ""
	}

	if strings.HasPrefix(dsn, "file:") {
		dsn = strings.TrimPrefix(dsn, "file:")
	}
	if idx := strings.Index(dsn, "?"); idx >= 0 {
		dsn = dsn[:idx]
	}
	if dsn == "" {
		return ""
	}

	clean := filepath.Clean(dsn)
	base := filepath.Base(clean)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = base
	}
	if name == "" {
		return filepath.Join(filepath.Dir(clean), stem)
	}

	return filepath.Join(filepath.Dir(clean), stem+"."+name)
}
