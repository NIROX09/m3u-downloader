// Package worker drives queued downloads through download and finalization.
package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Thiritin/m3u-downloader/internal/downloader"
	"github.com/Thiritin/m3u-downloader/internal/nas"
	"github.com/Thiritin/m3u-downloader/internal/pause"
	"github.com/Thiritin/m3u-downloader/internal/remux"
	"github.com/Thiritin/m3u-downloader/internal/store"
)

// SidecarFn writes poster/fanart files for the given job into its destination folder.
type SidecarFn func(ctx context.Context, j store.JobRow) error

// ResolveFn turns a job into a fully-qualified provider URL.
type ResolveFn func(j store.JobRow) (string, error)

type Worker struct {
	Store          *store.Store
	UserAgent      string
	MoviesDir      string // configured root for VOD output; must exist (mount check)
	SeriesDir      string // configured root for series output; must exist (mount check)
	FinalMoviesDir string // NAS/library destination for completed VOD files
	FinalSeriesDir string // NAS/library destination for completed series files
	ResolveURL     ResolveFn
	Sidecars       SidecarFn
	BackoffSeconds []int
	MaxRetries     int
	Remux          bool
	Logger         *slog.Logger
	NAS            *nas.Watchdog
	Pauses         *pause.Controller
	remuxFn        func(context.Context, string, string) error
}

var ErrPaused = errors.New("job paused by schedule")

const pipelineDownloadDelay = 5 * time.Second

func (w *Worker) log() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

// errPermanent wraps errors that must NOT be retried (disk full, dest unmounted).
type errPermanent struct{ err error }

func (e errPermanent) Error() string { return e.err.Error() }
func (e errPermanent) Unwrap() error { return e.err }

func isPermanent(err error) bool {
	var p errPermanent
	return errors.As(err, &p)
}

// errDestinationUnavailable represents a NAS or external-volume outage.
// It must requeue the active job without consuming an attempt or marking it failed.
type errDestinationUnavailable struct{ err error }

func (e errDestinationUnavailable) Error() string { return e.err.Error() }
func (e errDestinationUnavailable) Unwrap() error { return e.err }

func isDestinationUnavailable(err error) bool {
	var u errDestinationUnavailable
	return errors.As(err, &u)
}

// Run keeps provider downloads serial while allowing completed downloads to
// remux and transfer in the background.
func (w *Worker) Run(ctx context.Context) error {
	requeued, err := w.Store.RequeueInterruptedJobs(ctx)
	if err != nil {
		return fmt.Errorf("recover interrupted jobs: %w", err)
	}
	if requeued > 0 {
		w.log().Info("requeued interrupted jobs", "count", requeued)
	}
	if w.MaxRetries == 0 {
		w.MaxRetries = 3
	}
	if len(w.BackoffSeconds) == 0 {
		w.BackoffSeconds = []int{5, 30, 120}
	}
	if w.Remux && w.remuxFn == nil {
		if err := remux.EnsureFFmpeg(); err != nil {
			return err
		}
	}
	if w.NAS != nil {
		go w.NAS.Run(ctx)
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var pipelines sync.WaitGroup
	nextDownloadAt := time.Time{}
	for {
		select {
		case <-ctx.Done():
			pipelines.Wait()
			return ctx.Err()
		default:
		}

		if w.Pauses != nil {
			paused, err := w.Pauses.IsPaused(ctx, "downloads")
			if err != nil {
				w.log().Error("check download pause", "err", err)
				select {
				case <-ctx.Done():
					pipelines.Wait()
					return ctx.Err()
				case <-tick.C:
					continue
				}
			}
			if paused {
				select {
				case <-ctx.Done():
					pipelines.Wait()
					return ctx.Err()
				case <-tick.C:
					continue
				}
			}
		}

		if wait := time.Until(nextDownloadAt); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				pipelines.Wait()
				return ctx.Err()
			case <-timer.C:
			}
		}

		job, err := w.Store.ClaimNext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				pipelines.Wait()
				return ctx.Err()
			}
			w.log().Error("claim", "err", err)
			continue
		}
		if job == nil {
			select {
			case <-ctx.Done():
				pipelines.Wait()
				return ctx.Err()
			case <-tick.C:
				continue
			}
		}

		downloadComplete := make(chan struct{})
		pipelineDone := make(chan struct{})
		var signalOnce sync.Once
		pipelines.Add(1)
		go func(job store.JobRow) {
			defer pipelines.Done()
			defer close(pipelineDone)
			if err := w.processClaimedJob(ctx, job, func() {
				signalOnce.Do(func() { close(downloadComplete) })
			}); err != nil {
				w.log().Error("process job", "id", job.ID, "err", err)
			}
		}(*job)

		select {
		case <-downloadComplete:
			nextDownloadAt = time.Now().Add(pipelineDownloadDelay)
		case <-pipelineDone:
			select {
			case <-downloadComplete:
				nextDownloadAt = time.Now().Add(pipelineDownloadDelay)
			default:
			}
		case <-ctx.Done():
			pipelines.Wait()
			return ctx.Err()
		}
	}
}

func (w *Worker) processOne(ctx context.Context) error {
	if w.Pauses != nil {
		paused, err := w.Pauses.IsPaused(ctx, "downloads")
		if err != nil {
			return fmt.Errorf("check download pause: %w", err)
		}
		if paused {
			return nil
		}
	}
	job, err := w.Store.ClaimNext(ctx)
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}
	if job == nil {
		return nil
	}
	return w.processClaimedJob(ctx, *job, nil)
}

func (w *Worker) processClaimedJob(ctx context.Context, job store.JobRow, onDownloadComplete func()) error {
	w.log().Info("processing", "id", job.ID, "kind", job.Kind, "src", job.SourceID)

	runErr := w.runJob(ctx, job, onDownloadComplete)

	// If the user cancelled or deleted the job mid-flight, the runJob ctx
	// was cancelled by the per-job poller. Don't transition state — the
	// row is already at status='cancelled' (or gone). Just clean up.
	if status, _ := w.Store.GetJobStatus(ctx, job.ID); status != "active" {
		w.log().Info("job cancelled or removed by user", "id", job.ID, "status", status)
		_ = os.Remove(job.DestPath + ".raw")
		_ = os.Remove(job.DestPath + ".raw.part")
		return nil
	}

	if errors.Is(runErr, ErrPaused) {
		w.log().Info("download paused by schedule; preserving partial data", "id", job.ID)
		return w.Store.RequeueJobNoAttempt(ctx, job.ID, "paused by schedule")
	}

	if runErr == nil {
		_ = w.Store.SetJobPhase(ctx, job.ID, "completed")
		return w.Store.CompleteJob(ctx, job.ID)
	}
	w.log().Warn("job failed", "id", job.ID, "err", runErr)

	if isDestinationUnavailable(runErr) {
		w.log().Warn("destination unavailable; requeueing without retry attempt", "id", job.ID)
		return w.Store.RequeueJobNoAttempt(ctx, job.ID, runErr.Error())
	}
	if w.NAS != nil {
		if err := w.NAS.Check(job.Kind); err != nil {
			w.log().Warn("destination became unavailable; requeueing without retry attempt", "id", job.ID, "err", err)
			return w.Store.RequeueJobNoAttempt(ctx, job.ID, err.Error())
		}
	}

	// Provider connection-limit hit: wait and requeue WITHOUT consuming an attempt.
	if errors.Is(runErr, downloader.ErrConnectionLimit) {
		w.log().Info("connection limit hit; sleeping 30s before requeue", "id", job.ID)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
		}
		return w.Store.RequeueJobNoAttempt(ctx, job.ID, runErr.Error())
	}

	// Disk full and other permanent destination failures fail immediately.
	if isPermanent(runErr) {
		return w.Store.FailJob(ctx, job.ID, runErr.Error())
	}

	// Transient: retry with backoff if attempts remain.
	if job.Attempts < w.MaxRetries {
		backoffIdx := job.Attempts
		if backoffIdx >= len(w.BackoffSeconds) {
			backoffIdx = len(w.BackoffSeconds) - 1
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(w.BackoffSeconds[backoffIdx]) * time.Second):
		}
		return w.Store.RequeueJob(ctx, job.ID, runErr.Error())
	}
	return w.Store.FailJob(ctx, job.ID, runErr.Error())
}

func (w *Worker) runJob(parent context.Context, job store.JobRow, onDownloadComplete func()) error {
	if job.DestPath == "" {
		return errPermanent{errors.New("job has no dest_path")}
	}

	// Per-job context so the user can cancel an in-flight download from the
	// queue view. A goroutine polls the DB once per second; if the row's
	// status changes away from 'active' (cancelled or deleted), we cancel.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var paused atomic.Bool
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if w.Pauses != nil {
					isPaused, err := w.Pauses.IsPaused(parent, "downloads")
					if err != nil || isPaused {
						paused.Store(true)
						cancel()
						return
					}
				}
				status, err := w.Store.GetJobStatus(parent, job.ID)
				if err == nil && status != "active" {
					cancel()
					return
				}
			}
		}
	}()
	isPaused := func() bool {
		return paused.Load()
	}
	// Verify the configured library root exists. When a network/external volume
	// is unmounted, the mount-point dir often remains as an empty placeholder,
	// so we must check the EXACT configured root, not a parent of dest_path.
	root := w.MoviesDir
	if job.Kind == "episode" {
		root = w.SeriesDir
	}
	if root != "" {
		if _, err := os.Stat(root); err != nil {
			return errDestinationUnavailable{fmt.Errorf("temporary destination unavailable: %s: %w", root, err)}
		}
	}
	finalRoot := w.FinalMoviesDir
	processingRoot := w.MoviesDir
	if job.Kind == "episode" {
		finalRoot = w.FinalSeriesDir
		processingRoot = w.SeriesDir
	}
	if finalRoot == "" {
		finalRoot = root
	}
	if processingRoot == "" {
		processingRoot = root
	}
	relative, err := relativeLibraryPath(finalRoot, job.DestPath)
	if err != nil {
		relative, err = relativeLibraryPath(processingRoot, job.DestPath)
	}
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." ||
		len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return errPermanent{fmt.Errorf("job destination cannot be mapped to temporary or final library roots")}
	}
	tempDest := filepath.Join(processingRoot, relative) + ".processing"
	if err := os.MkdirAll(filepath.Dir(tempDest), 0o755); err != nil {
		return errPermanent{fmt.Errorf("mkdir temporary destination: %w", err)}
	}

	// If a previous run completed remux locally but could not reach the NAS,
	// retain that finished file and only retry its final copy.
	if fi, err := os.Stat(tempDest); err == nil && fi.Size() > 0 {
		if w.Pauses != nil {
			pausedNow, pauseErr := w.Pauses.IsPaused(ctx, "downloads")
			if pauseErr != nil || pausedNow {
				return ErrPaused
			}
		}
		if _, err := os.Stat(finalRoot); err != nil {
			return errDestinationUnavailable{fmt.Errorf("final destination unavailable: %s: %w", finalRoot, err)}
		}
		if onDownloadComplete != nil {
			onDownloadComplete()
		}
		_ = w.Store.SetJobPhase(ctx, job.ID, "transfer")
		if err := os.MkdirAll(filepath.Dir(job.DestPath), 0o755); err != nil {
			return errDestinationUnavailable{fmt.Errorf("mkdir final destination: %w", err)}
		}
		if err := copyCompletedFile(ctx, tempDest, job.DestPath); err != nil {
			if isPaused() {
				return ErrPaused
			}
			return errDestinationUnavailable{fmt.Errorf("copy completed file to library: %w", err)}
		}
		_ = os.Remove(tempDest)
		if w.Sidecars != nil {
			if err := w.Sidecars(ctx, job); err != nil {
				w.log().Warn("sidecars failed (non-fatal)", "id", job.ID, "err", err)
			}
		}
		return nil
	}

	url, err := w.ResolveURL(job)
	if err != nil {
		return fmt.Errorf("resolve url: %w", err)
	}

	tmp := tempDest + ".raw"
	d := &downloader.Downloader{UserAgent: w.UserAgent}
	_ = w.Store.SetJobPhase(ctx, job.ID, "download")
	progress := func(got, total int64) {
		_ = w.Store.UpdateProgress(ctx, job.ID, got, total)
	}
	if err := d.Download(ctx, url, tmp, progress); err != nil {
		if isPaused() {
			return ErrPaused
		}
		return fmt.Errorf("download: %w", err)
	}
	// Some providers list titles they don't actually have and respond
	// 200 OK with an empty body. Catch that before passing garbage to ffmpeg.
	if fi, err := os.Stat(tmp); err == nil && fi.Size() == 0 {
		_ = os.Remove(tmp)
		return errPermanent{fmt.Errorf("provider returned empty file (title not actually available)")}
	}

	if w.Remux {
		_ = w.Store.SetJobPhase(ctx, job.ID, "remux")
		if onDownloadComplete != nil {
			onDownloadComplete()
		}
		remuxFn := w.remuxFn
		if remuxFn == nil {
			remuxFn = remux.ToMKV
		}
		if err := remuxFn(ctx, tmp, tempDest); err != nil {
			if isPaused() {
				_ = os.Remove(tempDest)
				return ErrPaused
			}
			return err
		}
		_ = os.Remove(tmp)
	} else {
		if err := os.Rename(tmp, tempDest); err != nil {
			return err
		}
		if onDownloadComplete != nil {
			onDownloadComplete()
		}
	}
	if _, err := os.Stat(finalRoot); err != nil {
		return errDestinationUnavailable{fmt.Errorf("final destination unavailable: %s: %w", finalRoot, err)}
	}
	if w.Pauses != nil {
		pausedNow, err := w.Pauses.IsPaused(ctx, "downloads")
		if err != nil || pausedNow {
			return ErrPaused
		}
	}
	_ = w.Store.SetJobPhase(ctx, job.ID, "transfer")
	if err := os.MkdirAll(filepath.Dir(job.DestPath), 0o755); err != nil {
		return errDestinationUnavailable{fmt.Errorf("mkdir final destination: %w", err)}
	}
	if err := copyCompletedFile(ctx, tempDest, job.DestPath); err != nil {
		if isPaused() {
			return ErrPaused
		}
		return errDestinationUnavailable{fmt.Errorf("copy completed file to library: %w", err)}
	}
	_ = os.Remove(tempDest)

	if w.Sidecars != nil {
		if err := w.Sidecars(ctx, job); err != nil {
			w.log().Warn("sidecars failed (non-fatal)", "id", job.ID, "err", err)
		}
	}
	return nil
}

func relativeLibraryPath(root, dest string) (string, error) {
	root = filepath.Clean(root)
	dest = filepath.Clean(dest)
	rootVol := filepath.VolumeName(root)
	destVol := filepath.VolumeName(dest)
	if rootVol != "" && strings.EqualFold(rootVol, destVol) {
		rootRest := strings.TrimPrefix(root, rootVol)
		destRest := strings.TrimPrefix(dest, destVol)
		rootRest = strings.Trim(rootRest, `\/`)
		destRest = strings.Trim(destRest, `\/`)
		if rootRest == "" {
			if destRest == "" {
				return "", fmt.Errorf("destination matches root")
			}
			return filepath.FromSlash(destRest), nil
		}
		relative, err := filepath.Rel(rootRest, destRest)
		if err == nil && relative != "." && !filepath.IsAbs(relative) &&
			relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return relative, nil
		}
	}
	return filepath.Rel(root, dest)
}

func copyCompletedFile(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	part := dst + ".part"
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, &contextReader{ctx: ctx, reader: in}); err != nil {
		out.Close()
		_ = os.Remove(part)
		return err
	}

	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(part)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(part)
		return err
	}
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return err
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}
