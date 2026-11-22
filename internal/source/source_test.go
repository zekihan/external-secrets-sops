package source

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func TestGitLifecycle(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Git.Branch = "secrets"
	remote := testutil.InitRepo(t, cfg.Git.URL, "secrets")
	file := "generated/project/app/dev/values.yaml"
	first := testutil.Commit(
		t,
		remote,
		map[string][]byte{
			file: []byte("encrypted-one"),
			"generated/unrelated/app/dev/values.yaml":  []byte("ignored"),
			"generated/project/other/base/values.yaml": []byte("unused base"),
			"generated/project/other/prod/values.yaml": []byte("unselected"),
		},
	)
	r, err := New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := r.Fetch(t.Context())
	if err != nil || revision != first {
		t.Fatalf("fetch = %s %v", revision, err)
	}
	docs, err := r.Documents(t.Context(), revision)
	if err != nil || len(docs) != 1 {
		t.Fatalf("documents = %d %v", len(docs), err)
	}
	if _, err = os.Stat(filepath.Join(r.dir, "generated")); !os.IsNotExist(err) {
		t.Fatal("created worktree")
	}
	at := time.Now().UTC()
	if err = r.Save(t.Context(), first, at); err != nil {
		t.Fatal(err)
	}
	restored, _ := New(cfg, "")
	marker, err := restored.Cached()
	if err != nil || marker.Revision != first || !marker.ValidatedAt.Equal(at) {
		t.Fatal("cache recovery failed")
	}
	again, err := r.Fetch(t.Context())
	if err != nil || again != first {
		t.Fatal("unchanged fetch failed")
	}
	second := testutil.Commit(t, remote, map[string][]byte{file: []byte("encrypted-two")})
	if got, err := r.Fetch(t.Context()); err != nil || got != second {
		t.Fatal("advance failed")
	}
	if err = remote.Storer.SetReference(
		plumbing.NewHashReference(
			plumbing.NewBranchReferenceName("secrets"),
			plumbing.NewHash(first),
		),
	); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Fetch(t.Context()); err != nil || got != first {
		t.Fatalf("rollback failed: %v", err)
	}
	cfg.Scope.Environments = []string{"prod"}
	changed, _ := New(cfg, "")
	if _, err = changed.Cached(); err == nil {
		t.Fatal("scope mismatch accepted")
	}
	cfg.Git.Branch = "other"
	other, _ := New(cfg, "")
	if other.dir == r.dir {
		t.Fatal("branch cache reused")
	}
	if _, err = other.Fetch(t.Context()); err == nil {
		t.Fatal("missing branch accepted")
	}
	if err = os.Rename(cfg.Git.URL, cfg.Git.URL+"-offline"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(cfg.Git.URL+"-offline", cfg.Git.URL) })
	if _, err = r.Fetch(t.Context()); err == nil {
		t.Fatal("offline fetch succeeded")
	}
	if _, err = restored.Documents(t.Context(), marker.Revision); err != nil {
		t.Fatal("offline cache unavailable")
	}
}
func TestCorruptCacheAndSelectedPaths(t *testing.T) {
	cfg := testutil.Config(t)
	remote := testutil.InitRepo(t, cfg.Git.URL, "main")
	testutil.Commit(
		t,
		remote,
		map[string][]byte{"generated/project/app/dev/values.yaml": []byte("one")},
	)
	r, _ := New(cfg, "")
	if _, err := r.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "config"), []byte("broken ["), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered, _ := New(cfg, "")
	if _, err := recovered.Fetch(t.Context()); err != nil {
		t.Fatalf("corrupt cache did not recover: %v", err)
	}
	cfg.Limits.DocumentBytes = 1
	limited, _ := New(cfg, "")
	revision, err := limited.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = limited.Documents(t.Context(), revision); err == nil {
		t.Fatal("size limit ignored")
	}
	wt, _ := remote.Worktree()
	file := "generated/project/app/dev/values.yaml"
	if err = os.Remove(filepath.Join(cfg.Git.URL, file)); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("/etc/passwd", filepath.Join(cfg.Git.URL, file)); err != nil {
		t.Fatal(err)
	}
	if _, err = wt.Add(file); err != nil {
		t.Fatal(err)
	}
	testutil.Commit(t, remote, nil)
	revision, err = recovered.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recovered.Documents(t.Context(), revision); err == nil {
		t.Fatal("symlink accepted")
	}
	// A forced divergent commit must also replace the selected remote ref.
	if err = wt.Reset(&git.ResetOptions{Mode: git.HardReset}); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsInvalidBranch(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Git.Branch = "bad..branch"
	if _, err := New(cfg, ""); err == nil || err.Error() != "invalid Git branch" {
		t.Fatalf("New with invalid branch: %v", err)
	}
}

func TestSaveRequiresFetchedCandidate(t *testing.T) {
	cfg := testutil.Config(t)
	remote := testutil.InitRepo(t, cfg.Git.URL, "main")
	revision := testutil.Commit(
		t,
		remote,
		map[string][]byte{"generated/project/app/dev/values.yaml": []byte("one")},
	)
	r, err := New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	// No Fetch yet: there is no candidate to publish.
	if err = r.Save(t.Context(), revision, time.Now()); err == nil {
		t.Fatal("Save succeeded without a fetched candidate")
	}
	if _, err = r.Cached(); err == nil {
		t.Fatal("cache published without a fetched candidate")
	}
	fetched, err := r.Fetch(t.Context())
	if err != nil || fetched != revision {
		t.Fatalf("fetch = %s %v", fetched, err)
	}
	if err = r.Save(
		t.Context(), "0000000000000000000000000000000000000000", time.Now(),
	); err == nil {
		t.Fatal("Save succeeded for a revision that was not fetched")
	}
	// A canceled context must not publish the marker.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = r.Save(ctx, revision, time.Now()); err == nil {
		t.Fatal("Save succeeded with a canceled context")
	}
	if _, err = r.Cached(); err == nil {
		t.Fatal("canceled Save published the cache marker")
	}
	if err = r.Save(t.Context(), revision, time.Now()); err != nil {
		t.Fatalf("Save after cancellation: %v", err)
	}
}

func TestCachedRejectsTamperedMarker(t *testing.T) {
	cfg := testutil.Config(t)
	remote := testutil.InitRepo(t, cfg.Git.URL, "main")
	revision := testutil.Commit(
		t,
		remote,
		map[string][]byte{"generated/project/app/dev/values.yaml": []byte("one")},
	)
	r, err := New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	if err = r.Save(t.Context(), revision, at); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(r.dir, "validated.json")
	valid, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Marker){
		"future validation time": func(m *Marker) { m.ValidatedAt = time.Now().Add(time.Hour) },
		"unknown generation":     func(m *Marker) { m.Generation = "slot-9" },
		"non-hash revision":      func(m *Marker) { m.Revision = "SECRET_CANARY" },
		"wrong fingerprint":      func(m *Marker) { m.Fingerprint = "0" },
	} {
		t.Run(name, func(t *testing.T) {
			var m Marker
			if err := json.Unmarshal(valid, &m); err != nil {
				t.Fatal(err)
			}
			mutate(&m)
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(markerPath, b, 0o600); err != nil {
				t.Fatal(err)
			}
			fresh, _ := New(cfg, "")
			if _, err := fresh.Cached(); err == nil {
				t.Fatal("tampered marker accepted")
			} else if strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("error leaked marker content: %v", err)
			}
		})
	}
	if err := os.WriteFile(markerPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, _ := New(cfg, "")
	if _, err := fresh.Cached(); err == nil {
		t.Fatal("malformed marker accepted")
	}
	if _, err := fresh.Documents(t.Context(), revision); err == nil {
		t.Fatal("documents read despite malformed marker")
	}
	if _, err := fresh.Fetch(t.Context()); err == nil {
		t.Fatal("fetch proceeded despite malformed marker")
	}
}
