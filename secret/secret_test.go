package secret

import (
	"github.com/zekihan/external-secrets-sops/config"
	"os"
	"path/filepath"
	"testing"
)

func getProjectRoot() string {
	wd, _ := os.Getwd()
	// Traverse up or use known logic to detect root
	return filepath.Join(wd, "..") // adjust as needed
}

func setupTestConfig(t *testing.T) {
	t.Helper()

	config.GetConfig().GitConfig.RepoPath = filepath.Join(getProjectRoot(), "testdata")
	config.GetConfig().GitConfig.RepoURL = ""
	config.GetConfig().GitConfig.AccessToken = ""
	config.GetConfig().GitConfig.SecretsPath = "generated"
}

func TestGetSecret(t *testing.T) {
	setupTestConfig(t)
	secret, err := GetSecret("project1", "config1", "dev", "DATABASE_PASSWORD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret != "dev_secret_123" {
		t.Errorf("expected dev_secret_123, got %s", secret)
	}
}

func TestGetAllSecretsComputed(t *testing.T) {
	setupTestConfig(t)
	values, err := GetAllSecretsComputed("project1", "config1", "dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// base key should be present if exists in base
	if values["BASE_URL"] != "https://api.example.com" {
		t.Errorf("expected BASE_URL to be https://api.example.com, got %s", values["BASE_URL"])
	}

	if values["DATABASE_HOST"] != "dev-db.example.com" {
		t.Errorf("expected DATABASE_HOST to be dev-db.example.com, got %s", values["DATABASE_HOST"])
	}
}

func TestGetAllSecrets(t *testing.T) {
	setupTestConfig(t)
	values, err := GetAllSecrets("project1", "config1", "dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if values["DATABASE_HOST"] != "dev-db.example.com" {
		t.Errorf("expected DATABASE_HOST to be dev-db.example.com, got %s", values["DATABASE_HOST"])
	}
}

func TestGetAllSecrets_FileNotExist(t *testing.T) {
	setupTestConfig(t)
	values, err := GetAllSecrets("project1", "config1", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("expected empty map, got %v", values)
	}
}
