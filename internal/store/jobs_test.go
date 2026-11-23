package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestEnqueueAndClaim(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.EnqueueJob(ctx, "vod", 100, "/m/Foo (2020)/Foo (2020).mkv")
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Errorf("id = %d", id)
	}

	got, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("ClaimNext returned nil on a non-empty queue")
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want active", got.Status)
	}

	again, err := s.ClaimNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again != nil {
		t.Errorf("expected nil on empty pending: %+v", again)
	}
}

func TestEnqueueDuplicateBlocked(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.EnqueueJob(ctx, "vod", 100, "/m/x.mkv"); err != nil {
		t.Fatal(err)
	}
	_, err := s.EnqueueJob(ctx, "vod", 100, "/m/x.mkv")
	if err == nil {
		t.Fatal("expected duplicate enqueue to fail (unique active index)")
	}
	if !errors.Is(err, ErrAlreadyQueued) {
		t.Errorf("expected ErrAlreadyQueued, got %v", err)
	}
}

func TestCancelAndDeleteJob(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Cancel an active job
	id, _ := s.EnqueueJob(ctx, "vod", 100, "/p")
	if _, err := s.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelJob(ctx, id); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetJobStatus(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got != "cancelled" {
		t.Errorf("status = %q, want cancelled", got)
	}

	// Delete the cancelled row
	if err := s.DeleteJob(ctx, id); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetJobStatus(ctx, id)
	if got != "" {
		t.Errorf("after delete, status = %q, want empty", got)
	}

	// After delete the same source can be re-enqueued
	if _, err := s.EnqueueJob(ctx, "vod", 100, "/p2"); err != nil {
		t.Errorf("re-enqueue after delete: %v", err)
	}
}

func TestJobStatusBySource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id1, _ := s.EnqueueJob(ctx, "vod", 100, "/p1")
	_, _ = s.EnqueueJob(ctx, "episode", 200, "/p2")
	if _, err := s.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	statuses, err := s.JobStatusBySource(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses[JobStatusKey("vod", 100)]; got != "active" {
		t.Errorf("vod:100 = %q, want active", got)
	}
	if got := statuses[JobStatusKey("episode", 200)]; got != "pending" {
		t.Errorf("episode:200 = %q, want pending", got)
	}
	// Completing a job should reflect in the map.
	if err := s.CompleteJob(ctx, id1); err != nil {
		t.Fatal(err)
	}
	statuses, _ = s.JobStatusBySource(ctx)
	if got := statuses[JobStatusKey("vod", 100)]; got != "completed" {
		t.Errorf("after complete, vod:100 = %q, want completed", got)
	}
}

func TestRequeueJobNoAttemptDoesNotIncrement(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id, _ := s.EnqueueJob(ctx, "vod", 100, "/p")
	if _, err := s.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.RequeueJobNoAttempt(ctx, id, "connection limit"); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := s.DB().QueryRow(`SELECT attempts FROM jobs WHERE id=?`, id).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Errorf("attempts = %d, want 0 (no-attempt requeue should not increment)", attempts)
	}
}

func TestRequeueClearsStartedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id, _ := s.EnqueueJob(ctx, "vod", 100, "/p")
	if _, err := s.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.RequeueJob(ctx, id, "transient"); err != nil {
		t.Fatal(err)
	}
	var status string
	var startedAt *int64
	err := s.DB().QueryRow(`SELECT status, started_at FROM jobs WHERE id=?`, id).Scan(&status, &startedAt)
	if err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Errorf("status = %q", status)
	}
	if startedAt != nil {
		t.Errorf("started_at not cleared: %v", startedAt)
	}
}

func TestRemoveQueuedEpisodeJobsForSeriesAndSeason(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.ReplaceSeasonsAndEpisodes(ctx, 100,
		[]SeasonRow{{SeriesID: 100, SeasonNumber: 1}, {SeriesID: 100, SeasonNumber: 2}},
		[]EpisodeRow{
			{EpisodeID: 1001, SeriesID: 100, SeasonNumber: 1, EpisodeNum: 1},
			{EpisodeID: 1002, SeriesID: 100, SeasonNumber: 2, EpisodeNum: 1},
			{EpisodeID: 2001, SeriesID: 200, SeasonNumber: 1, EpisodeNum: 1},
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueJob(ctx, "episode", 1001, "/series/100/s1e1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueJob(ctx, "episode", 1002, "/series/100/s2e1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnqueueJob(ctx, "episode", 2001, "/series/200/s1e1"); err != nil {
		t.Fatal(err)
	}

	cancelled, err := s.RemoveQueuedEpisodeJobsForSeason(ctx, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 {
		t.Fatalf("season removed = %d, want 1", cancelled)
	}
	status, err := s.GetJobStatus(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if status != "" {
		t.Fatalf("season episode status = %q, want removed", status)
	}

	cancelled, err = s.RemoveQueuedEpisodeJobsForSeries(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 {
		t.Fatalf("series removed = %d, want 1", cancelled)
	}

	status, err = s.GetJobStatus(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("other series status = %q, want pending", status)
	}
}

func TestEnqueueEpisodeJobStoresSeriesMetadata(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.EnqueueEpisodeJob(ctx, "/series/Show/Season 01/Show-S01E02.mkv", EpisodeRow{
		EpisodeID:    3002,
		SeriesID:     300,
		SeasonNumber: 1,
		EpisodeNum:   2,
	})
	if err != nil {
		t.Fatal(err)
	}

	jobs, err := s.ListJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	job := jobs[0]
	if job.ID != id || job.SeriesID != 300 || job.SeasonNumber != 1 || job.EpisodeNumber != 2 {
		t.Fatalf("episode metadata = %+v", job)
	}

	cancelled, err := s.RemoveQueuedEpisodeJobsForSeries(ctx, 300)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 {
		t.Fatalf("series removed = %d, want 1", cancelled)
	}

	jobs, err = s.ListJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs after series removal = %+v", jobs)
	}
}

func TestRetryCancelledJob(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.EnqueueJob(ctx, "vod", 100, "/movies/Test/Test.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CancelJob(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.RetryJob(ctx, id); err != nil {
		t.Fatal(err)
	}

	status, err := s.GetJobStatus(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("status = %q, want pending", status)
	}

	var progress int64
	var lastError sql.NullString
	var startedAt, completedAt *int64
	err = s.DB().QueryRow(`
		SELECT progress_bytes, last_error, started_at, completed_at
		FROM jobs WHERE id=?`, id).Scan(&progress, &lastError, &startedAt, &completedAt)
	if err != nil {
		t.Fatal(err)
	}
	if progress != 0 || lastError.Valid || startedAt != nil || completedAt != nil {
		t.Fatalf("retry reset incomplete: progress=%d error=%q started=%v completed=%v",
			progress, lastError.String, startedAt, completedAt)
	}
}

func TestFailJobAfterMaxAttempts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id, _ := s.EnqueueJob(ctx, "vod", 100, "/p")
	if _, err := s.ClaimNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.FailJob(ctx, id, "boom"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := s.DB().QueryRow(`SELECT status FROM jobs WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
	if _, err := s.EnqueueJob(ctx, "vod", 100, "/p"); err != nil {
		t.Errorf("re-enqueue after failure: %v", err)
	}
}
