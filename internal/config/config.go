// Package config loads optional user configuration. Defaults work with no file.
//
// Precedence, highest first: command-line flags, environment variables, the
// user config file, built-in defaults. This package only handles the file and
// defaults; the CLI layers flags and env on top.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config mirrors the TOML shape in spec section 18.
type Config struct {
	Recording Recording `toml:"recording"`
	Privacy   Privacy   `toml:"privacy"`
	Review    Review    `toml:"review"`
}

type Recording struct {
	CaptureCwd   bool `toml:"capture_cwd"`
	CaptureFiles bool `toml:"capture_files"`
}

type Privacy struct {
	// Redact lists literal strings replaced before persistence.
	Redact []string `toml:"redact"`
	// RedactPatterns lists Go regular expressions replaced before persistence.
	RedactPatterns []string `toml:"redact_patterns"`
	// IgnorePaths lists glob rules; matching file references are dropped.
	IgnorePaths []string `toml:"ignore_paths"`
	// BuiltinSecretHeuristics enables the bundled credential-shape patterns.
	BuiltinSecretHeuristics bool `toml:"builtin_secret_heuristics"`
}

type Review struct {
	OpenBrowser     bool `toml:"open_browser"`
	IncludeFollowup bool `toml:"include_followup"`
}

// Default is the zero-config behavior.
func Default() Config {
	return Config{
		Recording: Recording{CaptureCwd: true, CaptureFiles: true},
		Privacy:   Privacy{BuiltinSecretHeuristics: true},
		Review:    Review{OpenBrowser: true, IncludeFollowup: true},
	}
}

// DefaultPath is <UserConfigDir>/blinken/config.toml, or $BLINKEN_CONFIG.
func DefaultPath() (string, error) {
	if p := os.Getenv("BLINKEN_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "blinken", "config.toml"), nil
}

// Load reads path over the defaults. A missing file is not an error.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	meta, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("config %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return cfg, fmt.Errorf("config %s: unknown key %s", path, undecoded[0])
	}
	return cfg, nil
}
