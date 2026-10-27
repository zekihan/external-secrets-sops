package offline

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func fixture(t *testing.T, files map[string]string) Options {
	t.Helper()
	root := t.TempDir()
	files[".dummy-secrets"] = "synthetic-only\n"
	for name, text := range files {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Options{
		Input:        "dummy",
		Root:         root,
		Projects:     []string{"apps", "shared", "automation", "terraform"},
		Environments: []string{"cluster-1", "cluster-2", "production"},
		Bundle:       "terraform/git/production",
		Limits:       config.Defaults().Limits,
	}
}

func TestSyntheticComposition(t *testing.T) {
	opts := fixture(t, map[string]string{
		"shared/git-ssh/production/values.yaml":     "PRIVATE_KEY: 'synthetic key'\nBASE64: {$template: '${PRIVATE_KEY | base64}'}\n",
		"automation/ansible/base/values.yaml":       "KEY: {$ref: shared/git-ssh/production/BASE64}\nUSER: base\nURL: {$template: 'ssh://${USER | urluserinfo}'}\nREMOVE: base\n",
		"automation/ansible/production/values.yaml": "USER: 'prod user'\nREMOVE: null\nEMPTY: ''\n",
		"apps/cloudflared/cluster-1/values.yaml":    "TUNNEL: one\n",
		"apps/cloudflared/cluster-2/values.yaml":    "TUNNEL: two\n",
		"terraform/git/production/values.yaml":      "PAYLOAD: {$template: '${automation/ansible/production/KEY | json}'}\nTUNNELS: {$template: '${apps/cloudflared/cluster-1/TUNNEL},${apps/cloudflared/cluster-2/TUNNEL}'}\n",
	})
	// Invalid identity settings must never be consulted for explicitly dummy input.
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "must-not-open"))
	t.Setenv("SOPS_AGE_KEY", "must-not-parse")
	t.Setenv("SOPS_AGE_KEY_CMD", "must-not-execute")
	got, err := Resolve(t.Context(), opts)
	encoded, _ := json.Marshal(base64.StdEncoding.EncodeToString([]byte("synthetic key")))
	if err != nil || len(got) != 2 || got["PAYLOAD"] != string(encoded) ||
		got["TUNNELS"] != "one,two" {
		t.Fatalf("composition mismatch: %v %v", got, err)
	}
	opts.Bundle = "automation/ansible/production"
	got, err = Resolve(t.Context(), opts)
	if err != nil || got["URL"] != "ssh://prod%20user" || got["EMPTY"] != "" {
		t.Fatalf("inheritance mismatch: %v %v", got, err)
	}
	if _, ok := got["REMOVE"]; ok {
		t.Fatal("null key retained")
	}
	opts.Environments = []string{"production", "cluster-1"}
	if got, err = Resolve(t.Context(), opts); err == nil || got != nil {
		t.Fatal("missing dependency environment accepted")
	}
}

func TestCandidateFailureProducesNoValues(t *testing.T) {
	for _, plain := range []string{
		"VALUE: {$ref: SECRET_CANARY}\n",
		"VALUE: {$ref: NEXT}\nNEXT: {$ref: VALUE}\n",
		"VALUE: one\nVALUE: two\n",
		"VALUE: {$template: '${OTHER | base64 | json}'}\nOTHER: safe\n",
		"VALUE: {$ref: OTHER}\nOTHER: null\n",
	} {
		t.Run(plain, func(t *testing.T) {
			opts := fixture(t, map[string]string{
				"terraform/git/production/values.yaml":  "OUTPUT: valid\n",
				"shared/invalid/production/values.yaml": plain,
			})
			got, err := Resolve(t.Context(), opts)
			if err == nil || got != nil {
				t.Fatal("invalid candidate exposed output")
			}
			if strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatal("descriptor contents leaked")
			}
		})
	}
}

func TestOfflineValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Options)
	}{
		{"input", func(o *Options) { o.Input = "auto" }},
		{"root", func(o *Options) { o.Root = "" }},
		{"duplicate project", func(o *Options) { o.Projects = []string{"terraform", "terraform"} }},
		{"duplicate environment", func(o *Options) { o.Environments = []string{"production", "production"} }},
		{"traversal", func(o *Options) { o.Bundle = "terraform/../production" }},
		{"outside project", func(o *Options) { o.Bundle = "private/git/production" }},
		{"outside environment", func(o *Options) { o.Bundle = "terraform/git/local" }},
		{"missing bundle", func(o *Options) { o.Bundle = "terraform/absent/production" }},
		{"dummy revision", func(o *Options) { o.Revision = strings.Repeat("a", 40) }},
		{"dummy repository", func(o *Options) { o.Repo = "." }},
		{"sops missing repository", func(o *Options) { o.Input = "sops" }},
		{"document limit", func(o *Options) { o.Limits.DocumentBytes = 1 }},
		{"invalid limits", func(o *Options) { o.Limits.DecryptWorkers = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := fixture(
				t,
				map[string]string{"terraform/git/production/values.yaml": "VALUE: canary\n"},
			)
			tc.change(&opts)
			if got, err := Resolve(t.Context(), opts); err == nil || got != nil {
				t.Fatal("invalid offline options accepted")
			}
		})
	}
}

func TestCommittedSOPSResolution(t *testing.T) {
	dir := t.TempDir()
	repo := testutil.InitRepo(t, dir, "main")
	name := "encrypted/runtime/terraform/git/production/values.yaml"
	first := testutil.Commit(
		t,
		repo,
		map[string][]byte{name: testutil.Encrypt(t, "VALUE: first\n")},
	)
	testutil.Commit(t, repo, map[string][]byte{name: testutil.Encrypt(t, "VALUE: second\n")})
	// A malformed working copy must not influence the selected immutable revision.
	if err := os.WriteFile(filepath.Join(dir, name), []byte("broken: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Input:        "sops",
		Repo:         dir,
		Revision:     first,
		Root:         "encrypted/runtime",
		Projects:     []string{"terraform"},
		Environments: []string{"production"},
		Bundle:       "terraform/git/production",
		Limits:       config.Defaults().Limits,
	}
	got, err := Resolve(t.Context(), opts)
	if err != nil || len(got) != 1 || got["VALUE"] != "first" {
		t.Fatalf("committed resolution = %v %v", got, err)
	}
	for _, revision := range []string{"HEAD", "main", first[:12], strings.Repeat("0", 40)} {
		opts.Revision = revision
		if got, err = Resolve(t.Context(), opts); err == nil || got != nil {
			t.Fatal("invalid or unavailable revision accepted")
		}
	}
	opts.Revision = first
	for _, root := range []string{"../encrypted", "/encrypted", "encrypted/../runtime", "encrypted\\runtime"} {
		opts.Root = root
		if got, err = Resolve(t.Context(), opts); err == nil || got != nil {
			t.Fatal("invalid encrypted root accepted")
		}
	}
}
