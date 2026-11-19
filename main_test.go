package main

import (
	"bufio"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

func TestCheckConfigRejectsHTTPForbiddenToken(t *testing.T) {
	const token = "synthetic\vtoken"
	// Parse a synthetic request in memory: the server's HTTP parser rejects this
	// token before the authorization handler could compare it.
	request := "GET / HTTP/1.1\r\nHost: example.invalid\r\nAuthorization: Bearer " + token + "\r\n\r\n"
	if _, err := http.ReadRequest(bufio.NewReader(strings.NewReader(request))); err == nil {
		t.Fatal("reproduction token unexpectedly accepted by HTTP parser")
	}
	t.Setenv("ESS_READER_TOKEN", token)
	t.Setenv("ESS_ADMIN_TOKEN", "synthetic-admin")
	t.Setenv("ESS_GIT_TOKEN", "")
	filename := filepath.Join(t.TempDir(), "config.yaml")
	text := "git:\n  url: ./repo\nscope:\n  projects: [project]\n  environments: [dev]\n"
	if err := os.WriteFile(filename, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(
		[]string{"check-config", "--config", filename},
		&stdout,
		&stderr,
	); code != 1 ||
		stdout.Len() != 0 {
		t.Fatalf("check-config returned %d with stdout %q", code, stdout.String())
	}
	if stderr.String() != "API tokens must not contain whitespace or control characters\n" {
		t.Fatal("check-config did not return the sanitized token validation error")
	}
}

func TestCheckConfigRejectsNullListEntries(t *testing.T) {
	t.Setenv("ESS_READER_TOKEN", "synthetic-reader")
	t.Setenv("ESS_ADMIN_TOKEN", "synthetic-admin")
	t.Setenv("ESS_GIT_TOKEN", "")
	t.Setenv("ESS_GITHUB_WEBHOOK_SECRET", "")
	const base = "git:\n  url: ./repo\n"
	const scope = "scope:\n  projects: [project]\n  environments: [dev]\n"
	for _, tc := range []struct {
		name, contents string
		wantCode       int
	}{
		{"null project", base + "scope:\n  projects: [project, null]\n  environments: [dev]\n", 1},
		{"null environment alias", base + "  tokenEnv: &missing null\n" +
			"scope:\n  projects: [project]\n  environments: [dev, *missing]\n", 1},
		{"null Redis address", base + scope +
			"redis:\n  enabled: true\n  passwordEnv: ''\n  addresses: ['redis.example.invalid:6379', ~]\n", 1},
		{"quoted null alias", base + "scope:\n  projects: [project, &name 'null']\n  environments: [dev, *name]\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(filename, []byte(tc.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := run([]string{"check-config", "--config", filename}, &stdout, &stderr)
			if code != tc.wantCode {
				t.Fatalf("check-config exit = %d, want %d, stdout = %q, stderr = %q",
					code, tc.wantCode, stdout.String(), stderr.String())
			}
			if tc.wantCode != 0 &&
				(stdout.Len() != 0 || stderr.String() != "invalid configuration YAML\n") {
				t.Fatal("expected only a configuration YAML error")
			}
		})
	}
}
