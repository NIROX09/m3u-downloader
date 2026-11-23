//go:build linux

// Package service installs and uninstalls the Linux systemd-user unit
// that runs `m3u-dl worker`. The macOS counterpart lives in
// service_darwin.go.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const unitName = "m3u-dl.service"
const webUnitName = "m3u-dl-web.service"

// packagedUnitPath is the location where .deb/.rpm install the unit.
// If this file exists, Install reuses it instead of writing a user-local copy.
const packagedUnitPath = "/usr/lib/systemd/user/m3u-dl.service"

// userUnitPath returns the per-user systemd unit path,
// honoring $XDG_CONFIG_HOME and falling back to $HOME/.config.
func userUnitPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "systemd", "user", unitName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", unitName), nil
}

func webUserUnitPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "systemd", "user", webUnitName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", webUnitName), nil
}

// unitFor returns the systemd unit file content for the given binary path.
// Pure function — no I/O, easy to test.
func unitFor(binPath string) string {
	return fmt.Sprintf(`[Unit]
Description=m3u-dl download worker
After=network-online.target

[Service]
ExecStart=%s worker
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`, binPath)
}

func webUnitFor(binPath string) string {
	return fmt.Sprintf(`[Unit]
Description=m3u-dl web interface
After=network-online.target

[Service]
ExecStart=%s web
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
`, binPath)
}

func Install() error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	bin, _ = filepath.Abs(bin)

	if _, err := os.Stat(packagedUnitPath); err != nil {
		unitPath, err := userUnitPath()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(unitPath, []byte(unitFor(bin)), 0o644); err != nil {
			return err
		}
		fmt.Printf("Installed: %s\n", unitPath)
	}

	webUnitPath, err := webUserUnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(webUnitPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(webUnitPath, []byte(webUnitFor(bin)), 0o644); err != nil {
		return err
	}

	if err := enableAndStart(unitName, webUnitName); err != nil {
		return err
	}
	fmt.Printf("Installed and started: %s\n", webUnitPath)
	return nil
}

func Uninstall() error {
	// Ignore disable errors — the unit may not be loaded.
	_ = exec.Command("systemctl", "--user", "disable", "--now", unitName, webUnitName).Run()

	unitPath, err := userUnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	webUnitPath, err := webUserUnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(webUnitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("Uninstalled: %s\n", unitPath)
	return nil
}

func enableAndStart(names ...string) error {
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w: %s", err, string(out))
	}
	args := append([]string{"--user", "enable", "--now"}, names...)
	if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl --user enable --now: %w: %s", err, string(out))
	}
	return nil
}
