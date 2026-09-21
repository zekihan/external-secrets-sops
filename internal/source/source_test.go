package source

import (
	"os"
	"path/filepath"
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
	if err = r.Save(first, at); err != nil {
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
