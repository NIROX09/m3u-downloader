package nas

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"time"
)

type Target struct {
	Name  string
	Share string
	Mount string
	Dir   string
}

type Watchdog struct {
	Movies            Target
	Series            Target
	CheckInterval     time.Duration
	ReconnectInterval time.Duration
	Logger            *slog.Logger
}

func (w *Watchdog) log() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

func (w *Watchdog) Check(kind string) error {
	target := w.Movies
	if kind == "episode" {
		target = w.Series
	}
	path := target.Dir
	if path == "" {
		path = target.Share
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s target unavailable: %s: %w", target.Name, path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s target is not a directory: %s", target.Name, path)
	}
	return nil
}

func (w *Watchdog) reconnect(target Target) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("automatic SMB reconnect is only supported on Windows")
	}
	if target.Mount == "" {
		cmd := exec.Command("net.exe", "use", target.Share, "/PERSISTENT:NO")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("reconnect %s share failed: %w", target.Name, err)
		}
		_ = out
	}

	_ = exec.Command("net.exe", "use", target.Mount, "/delete", "/y").Run()
	cmd := exec.Command("net.exe", "use", target.Mount, target.Share, "/PERSISTENT:NO")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reconnect %s share failed: %w", target.Name, err)
	}
	if err := w.CheckByTarget(target); err != nil {
		return err
	}
	w.log().Info("NAS share reconnected", "target", target.Name, "mount", target.Mount)
	_ = out
	return nil
}

func (w *Watchdog) CheckByTarget(target Target) error {
	path := target.Dir
	if path == "" {
		path = target.Share
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s target unavailable: %s: %w", target.Name, path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s target is not a directory: %s", target.Name, path)
	}
	return nil
}

func (w *Watchdog) Run(ctx context.Context) {
	if w.CheckInterval <= 0 {
		w.CheckInterval = 30 * time.Second
	}
	if w.ReconnectInterval <= 0 {
		w.ReconnectInterval = 60 * time.Second
	}
	checkTicker := time.NewTicker(w.CheckInterval)
	reconnectTicker := time.NewTicker(w.ReconnectInterval)
	defer checkTicker.Stop()
	defer reconnectTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-checkTicker.C:
			for _, target := range []Target{w.Movies, w.Series} {
				if target.Name == "" {
					continue
				}
				if err := w.CheckByTarget(target); err != nil {
					w.log().Warn("NAS target unavailable", "target", target.Name, "err", err)
				}
			}
		case <-reconnectTicker.C:
			for _, target := range []Target{w.Movies, w.Series} {
				if target.Name == "" {
					continue
				}
				if err := w.CheckByTarget(target); err == nil {
					continue
				}
				if err := w.reconnect(target); err != nil {
					w.log().Warn("NAS reconnect failed", "target", target.Name)
				}
			}
		}
	}
}
