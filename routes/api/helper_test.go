package api

import (
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/zekihan/external-secrets-sops/config"
	gitLocal "github.com/zekihan/external-secrets-sops/git"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func getProjectRoot() string {
	wd, _ := os.Getwd()
	// Traverse up or use known logic to detect root
	return filepath.Join(wd, "../..") // adjust as needed
}

func setupTestConfig(t *testing.T) {
	t.Helper()

	config.GetConfig().GitConfig.RepoPath = filepath.Join(getProjectRoot(), "testdata")
	config.GetConfig().GitConfig.RepoURL = ""
	config.GetConfig().GitConfig.AccessToken = ""
	config.GetConfig().GitConfig.SecretsPath = "generated"
}

func setupTestRepo(t *testing.T) (string, string, func()) {
	t.Helper()
	remoteTmpDir := t.TempDir()
	localTmpDir := t.TempDir()
	repo, _ := git.PlainInit(remoteTmpDir, false)

	// Create a file and commit it
	file := filepath.Join(remoteTmpDir, "test.txt")
	_ = os.WriteFile(file, []byte("hello"), 0600)
	w, _ := repo.Worktree()
	_, _ = w.Add("test.txt")
	_, _ = w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "tester",
			Email: "tester@example.com",
			When:  time.Now(),
		},
	})

	// Set config for the test
	origConfig := config.GetConfig().GitConfig
	config.GetConfig().GitConfig.RepoPath = localTmpDir
	config.GetConfig().GitConfig.RepoURL = remoteTmpDir // local path for clone
	config.GetConfig().GitConfig.AccessToken = ""
	config.GetConfig().GitConfig.PullInterval = time.Second

	_ = gitLocal.Clone()

	cleanup := func() {
		os.RemoveAll(remoteTmpDir)
		os.RemoveAll(localTmpDir)
		config.GetConfig().GitConfig = origConfig
	}
	return remoteTmpDir, localTmpDir, cleanup
}
