package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGetDefaultConfig(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.WebConfig.Host != "127.0.0.1" {
		t.Errorf("expected default host 127.0.0.1, got %s", cfg.WebConfig.Host)
	}

	if cfg.WebConfig.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.WebConfig.Port)
	}

	if cfg.LogConfig.Level != slog.LevelWarn {
		t.Errorf("expected default log level 2, got %d", cfg.LogConfig.Level)
	}

	if cfg.GitConfig.PullInterval != 15*time.Minute {
		t.Errorf("expected default pull interval 15m, got %v", cfg.GitConfig.PullInterval)
	}
}

func TestGetConfigFilePath_CONFIG_PATH(t *testing.T) {
	t.Setenv("CONFIG_PATH", "/tmp/test_config.yaml")

	got := getConfigFilePath(t.Context())
	if got != "/tmp/test_config.yaml" {
		t.Errorf("expected /tmp/test_config.yaml, got %s", got)
	}
}

func TestGetConfigFilePath_CONFIG_DIR(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CONFIG_DIR", tmpDir)

	expected := filepath.Join(tmpDir, "config.yaml")

	got := getConfigFilePath(t.Context())
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestGetConfigFilePath_Default(t *testing.T) {
	os.Unsetenv("CONFIG_PATH")
	os.Unsetenv("CONFIG_DIR")

	cwd, _ := os.Getwd()
	expected := filepath.Join(cwd, "config.yaml")

	got := getConfigFilePath(t.Context())
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestGetConfigFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "config.yaml")

	content := `
web:
  host: "0.0.0.0"
  port: 9090
log:
  level: "info"
  addSource: true
  path: "/tmp/log"
git:
  repoPath: "/tmp/repo"
  repoUrl: "https://github.com/example/repo"
  accessToken: "token"
  pullInterval: 10m
  secretsPath: "/tmp/secrets"
`

	err := os.WriteFile(cfgFile, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	t.Setenv("CONFIG_PATH", cfgFile)

	cfg := GetConfigFromFile(t.Context())
	if cfg.WebConfig.Host != "0.0.0.0" {
		t.Errorf("expected host 0.0.0.0, got %s", cfg.WebConfig.Host)
	}

	if cfg.WebConfig.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.WebConfig.Port)
	}

	if cfg.LogConfig.LevelStr != "info" {
		t.Errorf("expected log level info, got %s", cfg.LogConfig.LevelStr)
	}

	if !cfg.LogConfig.AddSource {
		t.Errorf("expected AddSource true, got false")
	}

	if cfg.GitConfig.PullInterval != 10*time.Minute {
		t.Errorf("expected pull_interval 10m, got %v", cfg.GitConfig.PullInterval)
	}

	if cfg.GitConfig.RepoURL != "https://github.com/example/repo" {
		t.Errorf("expected repo_url https://github.com/example/repo, got %s", cfg.GitConfig.RepoURL)
	}
}

func TestGetConfig(t *testing.T) {
	// Prepare a temp config file
	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "config.yaml")

	content := `
web:
  host: "1.2.3.4"
  port: 1234
log:
  level: "error"
  addMethodName: false
  path: "/tmp/log2"
git:
  repoPath: "/tmp/repo2"
  repoUrl: "https://github.com/example/repo2"
  accessToken: "token2"
  pullInterval: 5m
  secretsPath: "/tmp/secrets2"
`

	err := os.WriteFile(cfgFile, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}

	t.Setenv("CONFIG_PATH", cfgFile)

	// Set an env var to override a value
	t.Setenv("ESS__WEB_HOST", "9.9.9.9")

	// Reset package-level config cache
	// (simulate fresh start)
	// This is safe in single-threaded test
	// (if not, use t.Parallel() carefully)
	config = nil

	cfg := GetConfig()
	if cfg.WebConfig.Host != "9.9.9.9" {
		t.Errorf("expected host overridden by env to 9.9.9.9, got %s", cfg.WebConfig.Host)
	}

	if cfg.WebConfig.Port != 1234 {
		t.Errorf("expected port 1234, got %d", cfg.WebConfig.Port)
	}

	if cfg.LogConfig.LevelStr != "error" {
		t.Errorf("expected log level error, got %s", cfg.LogConfig.LevelStr)
	}

	if cfg.LogConfig.Level != slog.LevelError {
		t.Errorf("expected LevelError, got %d", cfg.LogConfig.Level)
	}

	if cfg.GitConfig.PullInterval != 5*time.Minute {
		t.Errorf("expected pullInterval 5m, got %v", cfg.GitConfig.PullInterval)
	}

	if cfg.GitConfig.RepoURL != "https://github.com/example/repo2" {
		t.Errorf(
			"expected repoUrl https://github.com/example/repo2, got %s",
			cfg.GitConfig.RepoURL,
		)
	}
}
