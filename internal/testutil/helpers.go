package testutil

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	"github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/cmd/sops/common"
	sopsconfig "github.com/getsops/sops/v3/config"
	yamlstore "github.com/getsops/sops/v3/stores/yaml"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/zekihan/external-secrets-sops/internal/config"
)

func Logger() *slog.Logger { return slog.New(slog.DiscardHandler) }
func Config(t testing.TB) config.Config {
	t.Helper()
	c := config.Defaults()
	c.Git.URL = t.TempDir()
	c.Git.CacheDir = t.TempDir()
	c.Git.Root = "generated"
	c.Scope = config.Scope{Projects: []string{"project"}, Environments: []string{"dev"}}
	return c
}

type Plain struct{}

func (Plain) Decrypt(ctx context.Context, b []byte) ([]byte, error) { return b, ctx.Err() }
func Encrypt(t testing.TB, plain string) []byte {
	t.Helper()
	store := yamlstore.NewStore(&sopsconfig.YAMLStoreConfig{Indent: 2})
	branches, err := store.LoadPlainFile([]byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	key, err := age.MasterKeyFromRecipient(
		"age17jc9ncn8vpajqjq3sfcpuyp30netw68qm3jnmez6asngdeg0zd6qcqv2nd",
	)
	if err != nil {
		t.Fatal(err)
	}
	tree := sops.Tree{
		Branches: branches,
		Metadata: sops.Metadata{KeyGroups: []sops.KeyGroup{{key}}, Version: "3.13.3"},
	}
	dataKey, errs := tree.GenerateDataKey()
	if len(errs) != 0 {
		t.Fatal("generate test key failed")
	}
	if err = common.EncryptTree(
		common.EncryptTreeOpts{Tree: &tree, Cipher: aes.NewCipher(), DataKey: dataKey},
	); err != nil {
		t.Fatal(err)
	}
	data, err := store.EmitEncryptedFile(tree)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func InitRepo(t testing.TB, dir, branch string) *git.Repository {
	t.Helper()
	r, err := git.PlainInitWithOptions(
		dir,
		&git.PlainInitOptions{
			InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName(branch)},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func Commit(t testing.TB, r *git.Repository, files map[string][]byte) string {
	t.Helper()
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		full := filepath.Join(wt.Filesystem.Root(), filepath.FromSlash(name))
		if data == nil {
			if _, err = wt.Remove(name); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(full, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err = wt.Add(name); err != nil {
			t.Fatal(err)
		}
	}
	hash, err := wt.Commit(
		"Update fixture",
		&git.CommitOptions{
			Author: &object.Signature{
				Name:  "Fixture",
				Email: "fixture@example.invalid",
				When:  time.Now(),
			},
			AllowEmptyCommits: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return hash.String()
}
func Eventually(t testing.TB, condition func() bool) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("condition did not become true")
		case <-ticker.C:
		}
	}
}
