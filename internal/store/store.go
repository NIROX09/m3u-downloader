// Package store wraps the SQLite database used as the shared queue
// and catalog cache between the TUI and worker.
package store

import (
	"context"
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

	if err := applySchemaAndMigrations(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func applySchemaAndMigrations(db *sql.DB) (err error) {
	conn, err := db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("get migration connection: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if _, rollbackErr := conn.ExecContext(ctx, `ROLLBACK`); rollbackErr != nil && err == nil {
			err = fmt.Errorf("rollback schema migration: %w", rollbackErr)
		}
	}()

	if _, err := conn.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if err := migrateSeriesDetails(conn); err != nil {
		return fmt.Errorf("migrate series details: %w", err)
	}
	if err := migrateJobEpisodeMetadata(conn); err != nil {
		return fmt.Errorf("migrate job episode metadata: %w", err)
	}
	if err := migrateJobQueueMetadata(conn); err != nil {
		return fmt.Errorf("migrate job queue metadata: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	committed = true
	return nil
}

func tableColumns(conn *sql.Conn, table string) (map[string]bool, error) {
	rows, err := conn.QueryContext(context.Background(), `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, colType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

// migrateSeriesDetails adds the details_fetched_at column to existing
// databases. It is safe to execute every time the application starts.
func migrateSeriesDetails(conn *sql.Conn) error {
	columns, err := tableColumns(conn, "series")
	if err != nil {
		return err
	}
	if columns["details_fetched_at"] {
		return nil
	}

	_, err = conn.ExecContext(context.Background(), `ALTER TABLE series ADD COLUMN details_fetched_at INTEGER`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func migrateJobEpisodeMetadata(conn *sql.Conn) error {
	columns, err := tableColumns(conn, "jobs")
	if err != nil {
		return err
	}

	for _, column := range []string{"series_id", "season_number", "episode_number"} {
		if columns[column] {
			continue
		}
		if _, err := conn.ExecContext(context.Background(), `ALTER TABLE jobs ADD COLUMN `+column+` INTEGER`); err != nil {
			return err
		}
	}

	_, err = conn.ExecContext(context.Background(), `
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

func migrateJobQueueMetadata(conn *sql.Conn) error {
	columns, err := tableColumns(conn, "jobs")
	if err != nil {
		return err
	}

	if !columns["queue_position"] {
		if _, err := conn.ExecContext(context.Background(), `ALTER TABLE jobs ADD COLUMN queue_position INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !columns["phase"] {
		if _, err := conn.ExecContext(context.Background(), `ALTER TABLE jobs ADD COLUMN phase TEXT NOT NULL DEFAULT 'queued'`); err != nil {
			return err
		}
	}
	if !columns["phase_started_at"] {
		if _, err := conn.ExecContext(context.Background(), `ALTER TABLE jobs ADD COLUMN phase_started_at INTEGER`); err != nil {
			return err
		}
	}

	_, err = conn.ExecContext(context.Background(), `
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
	_, err = conn.ExecContext(context.Background(), `
			UPDATE jobs
			SET phase='completed'
			WHERE status='completed' AND phase='queued'`)
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(context.Background(), `
			UPDATE jobs
			SET phase='download'
			WHERE status='active' AND phase='queued'`)
	return err
}
