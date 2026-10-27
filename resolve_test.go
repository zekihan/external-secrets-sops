package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCommandOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, ".dummy-secrets"),
		[]byte("synthetic-only\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"apps/app/production/values.yaml": "OUTPUT: {$ref: shared/source/KEY}\n", "shared/source/production/values.yaml": "KEY: 'synthetic value'\nPRIVATE: must-not-emit\n"} {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ESS_READER_TOKEN", "")
	t.Setenv("ESS_ADMIN_TOKEN", "")
	t.Setenv("ESS_GIT_TOKEN", "")
	t.Setenv("ESS_REDIS_PASSWORD", "")
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "unavailable"))
	args := []string{
		"resolve",
		"--input",
		"dummy",
		"--root",
		root,
		"--projects",
		"apps,shared",
		"--environments",
		"production",
		"--bundle",
		"apps/app/production",
	}
	var out, stderr bytes.Buffer
	if code := run(args, &out, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var got map[string]string
	if err := json.Unmarshal(
		out.Bytes(),
		&got,
	); err != nil || len(got) != 1 ||
		got["OUTPUT"] != "synthetic value" {
		t.Fatalf("unexpected output: %s (%v)", out.String(), err)
	}
	if strings.Contains(out.String(), "must-not-emit") {
		t.Fatal("dependency bundle leaked")
	}
	// Full candidate validation happens before any selected bundle is emitted.
	if err := os.WriteFile(
		filepath.Join(root, "shared/source/production/values.yaml"),
		[]byte("KEY: {$ref: PRIVATE_CANARY}"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if code := run(
		args,
		&out,
		&stderr,
	); code != 1 || out.Len() != 0 || stderr.Len() == 0 ||
		strings.Contains(stderr.String(), "PRIVATE_CANARY") {
		t.Fatalf("failure output: %d %q %q", code, out.String(), stderr.String())
	}
}

func TestResolveCommandArguments(t *testing.T) {
	for _, args := range [][]string{
		{"resolve", "--input", "dummy"},
		{"resolve", "--document-bytes", "PRIVATE_CANARY"},
		{"resolve", "--unknown", "PRIVATE_CANARY"},
		{"resolve", "--timeout", "0s"},
		{"resolve", "extra"},
	} {
		var out, stderr bytes.Buffer
		if code := run(
			args,
			&out,
			&stderr,
		); code == 0 || out.Len() != 0 || stderr.Len() == 0 ||
			strings.Contains(stderr.String(), "PRIVATE_CANARY") {
			t.Fatalf("bad arguments output: %d %q %q", code, out.String(), stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := run(
		[]string{"resolve", "--help"},
		&out,
		&stderr,
	); code != 0 || !strings.Contains(out.String(), "-bundle") ||
		stderr.Len() != 0 {
		t.Fatal("resolve help unavailable")
	}
}

type failedOutput struct{}

func (failedOutput) Write([]byte) (int, error) { return 0, errors.New("private writer error") }

func TestResolveCommandOutputFailure(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "apps/app/dev/values.yaml")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("KEY: safe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".dummy-secrets"),
		[]byte("synthetic-only\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := run(
		[]string{
			"resolve",
			"--input",
			"dummy",
			"--root",
			root,
			"--projects",
			"apps",
			"--environments",
			"dev",
			"--bundle",
			"apps/app/dev",
		},
		failedOutput{},
		&stderr,
	); code != 1 ||
		stderr.String() != "cannot write resolved bundle\n" {
		t.Fatal("output error unsanitized")
	}
}
