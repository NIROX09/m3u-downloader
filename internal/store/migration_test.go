package store

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

func TestOpen_ConcurrentLegacyMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		CREATE TABLE series (
			series_id INTEGER PRIMARY KEY,
			category_id INTEGER NOT NULL
		);
		CREATE TABLE jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			source_id INTEGER NOT NULL,
			status TEXT NOT NULL,
			dest_path TEXT,
			progress_bytes INTEGER NOT NULL DEFAULT 0,
			total_bytes INTEGER,
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			created_at INTEGER NOT NULL,
			started_at INTEGER,
			completed_at INTEGER
		);
		INSERT INTO jobs(kind,source_id,status,created_at)
		VALUES ('episode',1,'completed',1), ('episode',2,'pending',2);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	const parallelOpens = 8
	start := make(chan struct{})
	errs := make(chan error, parallelOpens)
	var wait sync.WaitGroup
	wait.Add(parallelOpens)
	for range parallelOpens {
		go func() {
			defer wait.Done()
			<-start
			st, err := Open(path)
			if err != nil {
				errs <- err
				return
			}
			errs <- st.Close()
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Open failed: %v", err)
		}
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	columns, err := tableColumns(mustConn(t, st.DB()), "jobs")
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{
		"series_id", "season_number", "episode_number",
		"queue_position", "phase", "phase_started_at",
	} {
		if !columns[column] {
			t.Errorf("jobs table is missing migrated column %q", column)
		}
	}
	var completedPhase string
	if err := st.DB().QueryRow(`SELECT phase FROM jobs WHERE source_id=1`).Scan(&completedPhase); err != nil {
		t.Fatal(err)
	}
	if completedPhase != "completed" {
		t.Errorf("completed job phase = %q, want completed", completedPhase)
	}
}

func mustConn(t *testing.T, db *sql.DB) *sql.Conn {
	t.Helper()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close database connection: %v", err)
		}
	})
	return conn
}
