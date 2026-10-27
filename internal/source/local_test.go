package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func dummyTree(t *testing.T) (config.Config, string) {
	t.Helper()
	cfg := config.Defaults()
	cfg.Scope = config.Scope{Projects: []string{"apps"}, Environments: []string{"cluster-1"}}
	root := t.TempDir()
	for name, content := range map[string]string{
		".dummy-secrets":                      "synthetic-only\n",
		"apps/app/base/values.yaml":           "BASE: one\n",
		"apps/app/cluster-1/values.yaml":      "VALUE: two\n",
		"apps/base-only/base/values.yaml":     "UNUSED: three\n",
		"apps/outside/production/values.yaml": "UNUSED: four\n",
	} {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg, root
}

func TestDummySelection(t *testing.T) {
	cfg, root := dummyTree(t)
	docs, err := DummyDocuments(t.Context(), cfg, root)
	if err != nil || len(docs) != 2 || docs[0].Environment != "cluster-1" ||
		docs[1].Environment != "base" {
		t.Fatalf("selection: %v %v", docs, err)
	}
	cfg.Scope.Environments = append(cfg.Scope.Environments, "base")
	docs, err = DummyDocuments(t.Context(), cfg, root)
	if err != nil || len(docs) != 3 {
		t.Fatalf("explicit base: %v %v", docs, err)
	}
	cfg.Limits.SnapshotBytes = 2
	if _, err = DummyDocuments(t.Context(), cfg, root); err == nil {
		t.Fatal("aggregate byte limit ignored")
	}
}

func TestDummyMarkerRequired(t *testing.T) {
	for _, text := range []string{"", "true\n", "synthetic-only", strings.Repeat("a", 65)} {
		cfg, root := dummyTree(t)
		if err := os.WriteFile(
			filepath.Join(root, ".dummy-secrets"),
			[]byte(text),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := DummyDocuments(t.Context(), cfg, root); err == nil {
			t.Fatal("invalid marker accepted")
		}
	}
	cfg, root := dummyTree(t)
	if err := os.Remove(filepath.Join(root, ".dummy-secrets")); err != nil {
		t.Fatal(err)
	}
	if _, err := DummyDocuments(t.Context(), cfg, root); err == nil {
		t.Fatal("missing marker accepted")
	}
}

func TestDummyRejectsSelectedSymlinks(t *testing.T) {
	for _, name := range []string{".", ".dummy-secrets", "apps", "apps/app", "apps/app/cluster-1", "apps/app/cluster-1/values.yaml"} {
		t.Run(name, func(t *testing.T) {
			cfg, root := dummyTree(t)
			target := filepath.Join(root, name)
			saved := filepath.Join(t.TempDir(), "saved")
			if err := os.Rename(target, saved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(saved, target); err != nil {
				t.Fatal(err)
			}
			if _, err := DummyDocuments(t.Context(), cfg, root); err == nil {
				t.Fatal("selected symlink accepted")
			}
		})
	}
}

func TestDummyCancellationAndEntryLimit(t *testing.T) {
	cfg, root := dummyTree(t)
	ctx := t.Context()
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := DummyDocuments(cancelCtx, cfg, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	for i := range maxTreeEntries {
		name := filepath.Join(root, "apps", fmt.Sprintf("unused-%d", i))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := DummyDocuments(ctx, cfg, root); err == nil {
		t.Fatal("directory entry ceiling ignored")
	}
}

func TestDummyAndCommittedScopeMatch(t *testing.T) {
	cfg, root := dummyTree(t)
	repo := testutil.InitRepo(t, t.TempDir(), "main")
	files := make(map[string][]byte)
	if err := filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)], err = os.ReadFile(name)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	revision := testutil.Commit(t, repo, files)
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Git.Root = "."
	for _, environments := range [][]string{{"cluster-1"}, {"cluster-1", "production"}, {"base", "production"}} {
		cfg.Scope.Environments = environments
		plain, err := DummyDocuments(t.Context(), cfg, root)
		if err != nil {
			t.Fatal(err)
		}
		committed, err := LocalDocuments(t.Context(), cfg, worktree.Filesystem.Root(), revision)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(plain, committed) {
			t.Fatalf("selection differs for %v", environments)
		}
	}
}
