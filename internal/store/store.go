// Package store wraps the SQLite database used as the shared queue
// and catalog cache between the TUI and worker.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path,
// applies the schema and migrations, and sets the required PRAGMAs.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir for db: %w", err)
	}

	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	if err := migrateSeriesDetails(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate series details: %w", err)
	}

	return &Store{db: db}, nil
}

// migrateSeriesDetails adds the details_fetched_at column to existing
// databases. It is safe to execute every time the application starts.
func migrateSeriesDetails(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(series)`)
	if err != nil {
		return err
	}

	found := false

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)

		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primaryKey); err != nil {
			rows.Close()
			return err
		}

		if name == "details_fetched_at" {
			found = true
		}
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if found {
		return nil
	}

	_, err = db.Exec(`ALTER TABLE series ADD COLUMN details_fetched_at INTEGER`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }
