package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/zekihan/external-secrets-sops/config"
	gitLocal "github.com/zekihan/external-secrets-sops/git"
)

func getProjectRoot() string {
	wd, _ := os.Getwd()
	// Traverse up or use known logic to detect root
	return filepath.Join(wd, "..", "..") // adjust as needed
}

func setupTestConfig(t *testing.T) {
	t.Helper()

	config.GetConfig().GitConfig.RepoPath = filepath.Join(getProjectRoot(), "testdata")
	config.GetConfig().GitConfig.RepoURL = ""
	config.GetConfig().GitConfig.AccessToken = ""
	config.GetConfig().GitConfig.SecretsPath = "generated"
}

func setupTestRepo(t *testing.T) string {
	t.Helper()
	remoteTmpDir := t.TempDir()
	localTmpDir := t.TempDir()
	repo, _ := git.PlainInit(remoteTmpDir, false)

	// Create a file and commit it
	file := filepath.Join(remoteTmpDir, "test.txt")
	_ = os.WriteFile(file, []byte("hello"), 0o600)
	workTree, _ := repo.Worktree()
	_, _ = workTree.Add("test.txt")
	commitOptions := new(git.CommitOptions)
	commitOptions.Author = &object.Signature{
		Name:  "tester",
		Email: "tester@example.com",
		When:  time.Now(),
	}
	_, _ = workTree.Commit("initial commit", commitOptions)

	// Set config for the test
	origConfig := config.GetConfig().GitConfig
	config.GetConfig().GitConfig.RepoPath = localTmpDir
	config.GetConfig().GitConfig.RepoURL = remoteTmpDir // local path for clone
	config.GetConfig().GitConfig.AccessToken = ""
	config.GetConfig().GitConfig.PullInterval = time.Second

	_ = gitLocal.Clone(t.Context())

	cleanup := func() {
		err := os.RemoveAll(remoteTmpDir)
		if err != nil {
			t.Errorf("failed to remove remote temp dir: %v", err)
		}

		err = os.RemoveAll(localTmpDir)
		if err != nil {
			t.Errorf("failed to remove local temp dir: %v", err)
		}

		config.GetConfig().GitConfig = origConfig
	}

	t.Cleanup(cleanup)

	return localTmpDir
}
