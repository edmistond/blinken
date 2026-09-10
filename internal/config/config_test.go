package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingFileGivesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || !cfg.Recording.CaptureCwd || !cfg.Review.IncludeFollowup || !cfg.Privacy.BuiltinSecretHeuristics {
		t.Fatalf("defaults not applied: %+v %v", cfg, err)
	}
}

func TestLoadOverridesAndRejectsUnknownKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[recording]\ncapture_cwd = false\n[privacy]\nredact = [\"hunter2\"]\nignore_paths = [\".env\", \"secrets/**\"]\n"), 0o600)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Recording.CaptureCwd || !cfg.Recording.CaptureFiles || len(cfg.Privacy.Redact) != 1 || len(cfg.Privacy.IgnorePaths) != 2 {
		t.Fatalf("unexpected: %+v", cfg)
	}
	os.WriteFile(p, []byte("[recording]\ncaptur_cwd = false\n"), 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("expected unknown key error")
	}
}
