package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Thiritin/m3u-downloader/internal/pause"
	"github.com/Thiritin/m3u-downloader/internal/store"
)

func TestRun_RequeuesInterruptedJobsOnStartup(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	id, err := st.EnqueueJob(context.Background(), "vod", 1, "/movies/test.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateProgress(context.Background(), id, 100, 1000); err != nil {
		t.Fatal(err)
	}

	controller := pause.New(st.DB())
	if _, err := controller.Create(context.Background(), pause.RuleInput{
		Scope: "downloads",
		Mode:  "indefinite",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &Worker{Store: st, Pauses: controller}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		status, err := st.GetJobStatus(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if status == "pending" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job status after worker startup = %q, want pending", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}

	status, err := st.GetJobStatus(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("job status = %q, want pending", status)
	}
	jobs, err := st.ListJobs(context.Background(), "pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ProgressBytes != 100 || jobs[0].Attempts != 0 {
		t.Fatalf("recovered jobs = %+v, want saved progress and no retry consumed", jobs)
	}
}

func TestProcessOne_VOD_HappyPath(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg required")
	}
	tsBytes, err := os.ReadFile("../../testdata/tiny.ts")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", strconv.Itoa(len(tsBytes)))
			return
		}
		w.Write(tsBytes)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "s.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	moviesDir := filepath.Join(tmp, "Movies")
	if err := os.MkdirAll(moviesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(moviesDir, "Test (2024)", "Test (2024).mkv")
	id, err := st.EnqueueJob(context.Background(), "vod", 1, dest)
	if err != nil {
		t.Fatal(err)
	}

	w := &Worker{
		Store:          st,
		UserAgent:      "LimePlayer",
		MoviesDir:      moviesDir,
		Remux:          true,
		MaxRetries:     3,
		BackoffSeconds: []int{1, 1, 1},
		ResolveURL: func(j store.JobRow) (string, error) {
			return srv.URL + "/movie", nil
		},
	}
	if err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("expected %s to exist: %v", dest, err)
	}
	jobs, _ := st.ListJobs(context.Background(), "completed")
	if len(jobs) != 1 || jobs[0].ID != id {
		t.Errorf("unexpected completed jobs: %+v", jobs)
	}
}

func TestProcessOne_DestUnmounted_Requeues(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "s.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// MoviesDir points at a directory that does NOT exist — simulates unmounted volume
	moviesDir := filepath.Join(tmp, "definitely-not-mounted", "Movies")
	dest := filepath.Join(moviesDir, "Foo (2024)", "Foo (2024).mkv")
	if _, err := st.EnqueueJob(context.Background(), "vod", 1, dest); err != nil {
		t.Fatal(err)
	}

	w := &Worker{
		Store:          st,
		UserAgent:      "LimePlayer",
		MoviesDir:      moviesDir,
		MaxRetries:     3,
		BackoffSeconds: []int{1, 1, 1},
		ResolveURL:     func(j store.JobRow) (string, error) { return "http://unused", nil },
	}
	if err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	jobs, _ := st.ListJobs(context.Background(), "pending")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 pending job, got %d", len(jobs))
	}
	if jobs[0].Attempts != 0 {
		t.Errorf("attempts = %d, want 0 (requeued without consuming an attempt)", jobs[0].Attempts)
	}
}

func TestRelativeLibraryPath_DriveRoot(t *testing.T) {
	relative, err := relativeLibraryPath(`Y:\`, `Y:\How I Met Your Mother\Season 01\How I Met Your Mother - S01E01.mkv`)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(`How I Met Your Mother`, `Season 01`, `How I Met Your Mother - S01E01.mkv`)
	if relative != want {
		t.Fatalf("relative = %q, want %q", relative, want)
	}
}

func TestProcessOne_PausePreservesPartialDownload(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	tempRoot := filepath.Join(t.TempDir(), "temp")
	finalRoot := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(finalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(finalRoot, "Example", "Example.mkv")
	id, err := st.EnqueueJob(context.Background(), "vod", 1, dest)
	if err != nil {
		t.Fatal(err)
	}

	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10000000")
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			return
		}
		requestStarted <- struct{}{}
		flusher, _ := w.(http.Flusher)
		chunk := make([]byte, 32*1024)
		for i := 0; i < 300; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer server.Close()

	controller := pause.New(st.DB())
	w := &Worker{
		Store:          st,
		MoviesDir:      tempRoot,
		FinalMoviesDir: finalRoot,
		Pauses:         controller,
		ResolveURL:     func(store.JobRow) (string, error) { return server.URL, nil },
	}
	done := make(chan error, 1)
	go func() { done <- w.processOne(context.Background()) }()
	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("download request did not start")
	}
	if _, err := controller.Create(context.Background(), pause.RuleInput{
		Scope: "downloads", Mode: "indefinite",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("processOne: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("active download did not pause")
	}

	status, err := st.GetJobStatus(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("status = %q, want pending", status)
	}
	partial := filepath.Join(tempRoot, "Example", "Example.mkv.processing.raw.part")
	info, err := os.Stat(partial)
	if err != nil {
		t.Fatalf("partial download not preserved: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("partial download is empty")
	}
}

func TestProcessOne_CompletedTempFileWaitsForNAS(t *testing.T) {
	tmp := t.TempDir()
	st, err := store.Open(filepath.Join(tmp, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	processingRoot := filepath.Join(tmp, "Temp", "Movies")
	finalRoot := filepath.Join(tmp, "NAS", "Movies")
	if err := os.MkdirAll(processingRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(finalRoot, "Test (2024)", "Test (2024).mkv")
	id, err := st.EnqueueJob(context.Background(), "vod", 1, dest)
	if err != nil {
		t.Fatal(err)
	}

	relative, err := filepath.Rel(finalRoot, dest)
	if err != nil {
		t.Fatal(err)
	}
	tempDest := filepath.Join(processingRoot, relative) + ".processing"
	if err := os.MkdirAll(filepath.Dir(tempDest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempDest, []byte("finished-remux"), 0o755); err != nil {
		t.Fatal(err)
	}

	w := &Worker{
		Store:          st,
		UserAgent:      "LimePlayer",
		MoviesDir:      processingRoot,
		FinalMoviesDir: finalRoot,
		MaxRetries:     3,
		BackoffSeconds: []int{1, 1, 1},
		ResolveURL:     func(j store.JobRow) (string, error) { return "http://unused", nil },
	}
	if err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne while NAS unavailable: %v", err)
	}
	pending, _ := st.ListJobs(context.Background(), "pending")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending job, got %d", len(pending))
	}

	if err := os.MkdirAll(finalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne after NAS return: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("completed file was not copied to final destination: %v", err)
	}
	completed, _ := st.ListJobs(context.Background(), "completed")
	if len(completed) != 1 || completed[0].ID != id {
		t.Fatalf("unexpected completed jobs: %+v", completed)
	}
	if _, err := os.Stat(tempDest); !os.IsNotExist(err) {
		t.Fatalf("temporary completed file should be removed, stat err=%v", err)
	}
}
