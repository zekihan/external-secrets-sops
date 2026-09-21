package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCommands(t *testing.T) {
	var out, stderr bytes.Buffer
	if code := run(
		[]string{"version"},
		&out,
		&stderr,
	); code != 0 ||
		!bytes.Contains(out.Bytes(), []byte("external-secrets-sops")) {
		t.Fatal("version command failed")
	}
	if code := run([]string{"unknown"}, &out, &stderr); code != 2 {
		t.Fatal("unknown command accepted")
	}
	t.Setenv("ESS_READER_TOKEN", "reader")
	t.Setenv("ESS_ADMIN_TOKEN", "admin")
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(
		file,
		[]byte(
			"git:\n  url: https://nonexistent.invalid/repo\nscope:\n  projects: [p]\n  environments: [dev]\n",
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"check-config", "--config", file}, &out, &stderr); code != 0 {
		t.Fatalf("offline check failed: %s", stderr.String())
	}
}
