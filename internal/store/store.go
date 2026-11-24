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
	if err := migrateJobEpisodeMetadata(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate job episode metadata: %w", err)
	}
	if err := migrateJobQueueMetadata(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate job queue metadata: %w", err)
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

func migrateJobEpisodeMetadata(db *sql.DB) error {
	columns := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(jobs)`)
	if err != nil {
		return err
	}
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
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, column := range []string{"series_id", "season_number", "episode_number"} {
		if columns[column] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN ` + column + ` INTEGER`); err != nil {
			return err
		}
	}

	_, err = db.Exec(`
		UPDATE jobs
		SET series_id=(
				SELECT e.series_id FROM episodes e WHERE e.episode_id=jobs.source_id
			),
			season_number=(
				SELECT e.season_number FROM episodes e WHERE e.episode_id=jobs.source_id
			),
			episode_number=(
				SELECT e.episode_num FROM episodes e WHERE e.episode_id=jobs.source_id
			)
		WHERE kind='episode'
		  AND (series_id IS NULL OR season_number IS NULL OR episode_number IS NULL)`)
	return err
}

func migrateJobQueueMetadata(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(jobs)`)
	if err != nil {
		return err
	}
	foundQueuePosition := false
	foundPhase := false
	foundPhaseStarted := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, colType string
		var defaultV interface{}
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		switch name {
		case "queue_position":
			foundQueuePosition = true
		case "phase":
			foundPhase = true
		case "phase_started_at":
			foundPhaseStarted = true
		}
	}
	rows.Close()

	if !foundQueuePosition {
		if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN queue_position INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !foundPhase {
		if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN phase TEXT NOT NULL DEFAULT 'queued'`); err != nil {
			return err
		}
	}
	if !foundPhaseStarted {
		if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN phase_started_at INTEGER`); err != nil {
			return err
		}
	}

	_, err = db.Exec(`
			UPDATE jobs
			SET queue_position=(
				SELECT COUNT(*) FROM jobs earlier
				WHERE earlier.created_at < jobs.created_at
				   OR (earlier.created_at = jobs.created_at AND earlier.id <= jobs.id)
			)
			WHERE queue_position=0`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
			UPDATE jobs
			SET phase='completed'
			WHERE status='completed' AND phase='queued'`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
			UPDATE jobs
			SET phase='download'
			WHERE status='active' AND phase='queued'`)
	return err
}
