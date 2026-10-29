package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestMemoryResolveCommand(t *testing.T) {
	args := []string{
		"--input",
		"memory",
		"--projects",
		"apps,shared",
		"--environments",
		"production",
		"--bundle",
		"apps/app/production",
	}
	valid := `{"apps/app/production/values.yaml":"OUTPUT: {$template: '${shared/source/KEY | sha256}'}\n","shared/source/base/values.yaml":"KEY: synthetic\n","shared/source/production/values.yaml":"{}\n"}`
	for _, input := range []string{valid, `{"apps/app/production/values.yaml":"SECRET_CANARY: {$ref: missing}"}`, `{"../SECRET_CANARY/production/values.yaml":"x"}`, `{"records/private/production/values.yaml":"x"}`, `{"apps/app/production/values.yaml":"x","apps/app/production/values.yaml":"y"}`, valid + `{}`, `null`, `{"apps/app/production/values.yaml":null}`} {
		var out, stderr bytes.Buffer
		code := runResolveInput(args, strings.NewReader(input), &out, &stderr)
		if input == valid {
			if code != 0 || stderr.Len() != 0 ||
				!strings.Contains(
					out.String(),
					"b3cc0475bb78a5026098858e9889acf666d31062d513d303314eca31d36e72f2",
				) {
				t.Fatalf("memory contract failed: %d %s %s", code, out.String(), stderr.String())
			}
		} else if code == 0 || out.Len() != 0 || strings.Contains(stderr.String(), "SECRET_CANARY") {
			t.Fatalf(
				"invalid memory input accepted or leaked: %d %s %s",
				code,
				out.String(),
				stderr.String(),
			)
		}
	}
}

func TestMemoryResolveLimits(t *testing.T) {
	input := `{"apps/app/production/values.yaml":"KEY: synthetic\n"}`
	base := []string{
		"--input",
		"memory",
		"--projects",
		"apps",
		"--environments",
		"production",
		"--bundle",
		"apps/app/production",
	}
	for _, extra := range [][]string{{"--document-bytes", "8"}, {"--root", "unavailable"}, {"--repo", "unavailable"}, {"--revision", strings.Repeat("a", 40)}, {"--document-bytes", "8", "--snapshot-bytes", "8"}} {
		var out, stderr bytes.Buffer
		args := append(append([]string{}, base...), extra...)
		if code := runResolveInput(
			args,
			strings.NewReader(input),
			&out,
			&stderr,
		); code == 0 || out.Len() != 0 ||
			stderr.Len() == 0 {
			t.Fatal("memory input limits ignored")
		}
	}
}

func TestMemoryResolveStdinDeadline(t *testing.T) {
	if os.Getenv("ESS_TEST_MEMORY_STDIN") == "1" {
		os.Args = []string{
			os.Args[0], "resolve", "--input", "memory", "--projects", "apps",
			"--environments", "dev", "--bundle", "apps/app/dev", "--timeout", "50ms",
		}
		main()
	}
	for name, input := range map[string]string{
		"empty":                "",
		"partial":              `{"apps/app/dev/values.yaml":"SECRET_CANARY`,
		"complete without EOF": `{"apps/app/dev/values.yaml":"KEY: synthetic\n"}`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(
				ctx,
				os.Args[0],
				"-test.run=^TestMemoryResolveStdinDeadline$",
			)
			cmd.Env = append(os.Environ(), "ESS_TEST_MEMORY_STDIN=1")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if _, err = stdin.Write([]byte(input)); err != nil {
				t.Fatal(err)
			}
			err = cmd.Wait()
			if ctx.Err() != nil {
				t.Fatal("stdin read ignored resolution deadline")
			}
			var failure *exec.ExitError
			if !errors.As(err, &failure) || failure.ExitCode() != 1 || stdout.Len() != 0 ||
				stderr.Len() == 0 ||
				strings.Contains(stderr.String(), "SECRET_CANARY") {
				t.Fatalf(
					"cancellation contract failed: %v stdout=%q stderr=%q",
					err,
					stdout.String(),
					stderr.String(),
				)
			}
		})
	}
}

func TestMemoryResolveUnicodeIntegrity(t *testing.T) {
	args := []string{
		"--input", "memory", "--projects", "apps", "--environments", "dev",
		"--bundle", "apps/app/dev",
	}
	for _, tc := range []struct {
		name, encoded, want string
	}{
		{"invalid UTF-8", "\xff", ""},
		{"truncated UTF-8", "\xe2\x82", ""},
		{"lone high surrogate", `\ud800`, ""},
		{"lone low surrogate", `\udc00`, ""},
		{"high followed by scalar", `\ud800\u0041`, ""},
		{"reversed pair", `\udc00\ud800`, ""},
		{"valid pair", `\ud83d\udd10`, "🔐"},
		{"literal Unicode", "ş🔐", "ş🔐"},
		{"replacement rune", `\ufffd`, "�"},
		{"escaped backslash", `\\ud800`, `\ud800`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := `{"apps/app/dev/values.yaml":"KEY: '` + tc.encoded + `'\n"}`
			var out, stderr bytes.Buffer
			code := runResolveInput(args, strings.NewReader(input), &out, &stderr)
			if tc.want == "" {
				if code != 1 || out.Len() != 0 || stderr.String() != "invalid memory documents\n" {
					t.Fatalf(
						"malformed Unicode accepted: code=%d stdout=%q stderr=%q",
						code,
						out.String(),
						stderr.String(),
					)
				}
				return
			}
			var got map[string]string
			if err := json.Unmarshal(
				out.Bytes(),
				&got,
			); err != nil || code != 0 || stderr.Len() != 0 ||
				got["KEY"] != tc.want {
				t.Fatalf(
					"Unicode changed: code=%d stdout=%q stderr=%q error=%v",
					code,
					out.String(),
					stderr.String(),
					err,
				)
			}
		})
	}
}
