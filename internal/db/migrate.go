package db

import (
	"database/sql"
	"fmt"
)

func Migrate(db *sql.DB) error {
	migrations := []struct {
		version int
		sql     string
	}{
		{1, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`},
		{2, `CREATE TABLE IF NOT EXISTS servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			command TEXT NOT NULL,
			args TEXT NOT NULL DEFAULT '[]',
			env TEXT NOT NULL DEFAULT '{}',
			tags TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`},
		{3, `CREATE TABLE IF NOT EXISTS tokens (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			hash TEXT NOT NULL UNIQUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME
		)`},
		{4, `ALTER TABLE servers ADD COLUMN transport TEXT NOT NULL DEFAULT 'stdio'`},
		{5, `ALTER TABLE servers ADD COLUMN install TEXT NOT NULL DEFAULT '{}'`},
		{6, `ALTER TABLE servers ADD COLUMN platforms TEXT NOT NULL DEFAULT '[]'`},
	}

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`)
	if err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, m.version); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
	}
	return nil
}
