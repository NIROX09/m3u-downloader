// Package config loads and validates the m3u-dl TOML configuration file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Provider struct {
		BaseURL   string `toml:"base_url"`
		Username  string `toml:"username"`
		Password  string `toml:"password"`
		UserAgent string `toml:"user_agent"`
	} `toml:"provider"`
	Web struct {
		ListenAddr string `toml:"listen_addr"`
	} `toml:"web"`
	Output struct {
		MoviesDir string `toml:"movies_dir"`
		SeriesDir string `toml:"series_dir"`
	} `toml:"output"`
	Downloader struct {
		Remux          bool  `toml:"remux"`
		MaxRetries     int   `toml:"max_retries"`
		BackoffSeconds []int `toml:"backoff_seconds"`
	} `toml:"downloader"`
	NAS struct {
		Enabled                  bool   `toml:"enabled"`
		MoviesShare              string `toml:"movies_share"`
		SeriesShare              string `toml:"series_share"`
		MoviesMount              string `toml:"movies_mount"`
		SeriesMount              string `toml:"series_mount"`
		CheckIntervalSeconds     int    `toml:"check_interval_seconds"`
		ReconnectIntervalSeconds int    `toml:"reconnect_interval_seconds"`
	} `toml:"nas"`
}

// DefaultPath returns the platform-appropriate config path.
//
//   - Windows:       %APPDATA%/m3u-dl/config.toml
//   - macOS / Linux: $HOME/.config/m3u-dl/config.toml
//
// macOS uses the XDG path (not Library/Application Support) so config
// stays consistent between the two Unix-like platforms.
func DefaultPath() (string, error) {
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "m3u-dl", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "m3u-dl", "config.toml"), nil
}

// Load reads, parses, and validates a config file.
func Load(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate fills defaults and returns an error if required fields are missing.
func (c *Config) Validate() error {
	if c.Web.ListenAddr == "" {
		c.Web.ListenAddr = "0.0.0.0:8765"
	}
	if c.Provider.BaseURL == "" {
		return fmt.Errorf("provider.base_url is required")
	}
	if c.Provider.Username == "" {
		return fmt.Errorf("provider.username is required")
	}
	if c.Provider.Password == "" {
		return fmt.Errorf("provider.password is required")
	}
	if c.Output.MoviesDir == "" {
		return fmt.Errorf("output.movies_dir is required")
	}
	if c.Output.SeriesDir == "" {
		return fmt.Errorf("output.series_dir is required")
	}
	if c.Provider.UserAgent == "" {
		c.Provider.UserAgent = "LimePlayer"
	}
	if c.Downloader.MaxRetries == 0 {
		c.Downloader.MaxRetries = 3
	}
	if len(c.Downloader.BackoffSeconds) == 0 {
		c.Downloader.BackoffSeconds = []int{5, 30, 120}
	}
	if c.NAS.CheckIntervalSeconds == 0 {
		c.NAS.CheckIntervalSeconds = 30
	}
	if c.NAS.ReconnectIntervalSeconds == 0 {
		c.NAS.ReconnectIntervalSeconds = 60
	}
	if c.NAS.Enabled {
		if c.NAS.MoviesShare == "" {
			return fmt.Errorf("nas.movies_share is required when nas.enabled is true")
		}
		if c.NAS.SeriesShare == "" {
			return fmt.Errorf("nas.series_share is required when nas.enabled is true")
		}
	}
	return nil
}
