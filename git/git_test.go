package git

import (
	"github.com/zekihan/external-secrets-sops/config"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

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
	commitObj, _ := w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "tester",
			Email: "tester@example.com",
			When:  time.Now(),
		},
	})

	// Create 'main' branch pointing to the initial commit and checkout to it
	mainRefName := plumbing.NewBranchReferenceName("main")
	_ = repo.Storer.SetReference(plumbing.NewHashReference(mainRefName, commitObj))
	_ = w.Checkout(&git.CheckoutOptions{Branch: mainRefName})

	// Set config for the test
	origConfig := config.GetConfig().GitConfig
	config.GetConfig().GitConfig.RepoPath = localTmpDir
	config.GetConfig().GitConfig.RepoURL = remoteTmpDir // local path for clone
	config.GetConfig().GitConfig.AccessToken = ""
	config.GetConfig().GitConfig.PullInterval = time.Second

	cleanup := func() {
		os.RemoveAll(remoteTmpDir)
		os.RemoveAll(localTmpDir)
		config.GetConfig().GitConfig = origConfig
	}
	return remoteTmpDir, localTmpDir, cleanup
}

func TestIsDirExists(t *testing.T) {
	remoteTmpDir, localTmpDir, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)

	gitDir := filepath.Join(remoteTmpDir, ".git")
	if !isDirExists(gitDir) {
		t.Errorf(".git dir should exist")
	}
	if isDirExists(filepath.Join(localTmpDir, "not-exist")) {
		t.Errorf("non-existent dir should not exist")
	}
}

func TestIsRepoReady(t *testing.T) {
	_, _, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)

	err := Clone()
	if err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	if !IsRepoReady() {
		t.Errorf("repo should be ready")
	}
}

func TestGetLastCommit(t *testing.T) {
	_, _, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)

	err := Clone()
	if err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	commit, err := GetLastCommit()
	if err != nil {
		t.Fatalf("GetLastCommit failed: %v", err)
	}
	if commit.Message != "initial commit" {
		t.Errorf("unexpected commit message: %s", commit.Message)
	}
}

func TestCloneAndPull(t *testing.T) {
	_, _, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)

	err := Clone()
	if err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	if !IsRepoReady() {
		t.Errorf("repo should be ready after clone")
	}

	err = Pull()
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
}

func TestStartAndStopPullLoop(t *testing.T) {
	_, _, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)

	go StartPullLoop()
	time.Sleep(2 * time.Second)
	StopPullLoop()
}

func TestClone_EmptyRepoURL(t *testing.T) {
	_, localTmpDir, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)

	origRepoURL := config.GetConfig().GitConfig.RepoURL
	config.GetConfig().GitConfig.RepoURL = ""
	config.GetConfig().GitConfig.RepoPath = localTmpDir

	err := Clone()
	if err == nil || err.Error() != "repo_url is empty" {
		t.Errorf("expected error 'repo_url is empty', got %v", err)
	}
	config.GetConfig().GitConfig.RepoURL = origRepoURL
}

func TestClone_ExistingGitDir(t *testing.T) {
	_, _, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)

	// First clone to create .git in localTmpDir
	err := Clone()
	if err != nil {
		t.Fatalf("initial clone failed: %v", err)
	}

	// Now, .git exists. Clone again should be a no-op (no error, no re-clone)
	err = Clone()
	if err != nil {
		t.Errorf("expected no error when .git exists and remote matches, got %v", err)
	}
}

func TestPull_NotAlreadyUpToDate(t *testing.T) {
	remoteTmpDir, localTmpDir, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)

	// Initial clone
	err := Clone()
	if err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	repo, err := git.PlainOpen(remoteTmpDir)
	if err != nil {
		t.Fatalf("failed to open remote repo: %v", err)
	}
	file := filepath.Join(remoteTmpDir, "new.txt")
	_ = os.WriteFile(file, []byte("new content"), 0600)
	w, _ := repo.Worktree()
	_, _ = w.Add("new.txt")
	_, _ = w.Commit("new commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "tester",
			Email: "tester@example.com",
			When:  time.Now(),
		},
	})

	// Pull should fetch the new commit
	err = Pull()
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}

	// Check that the new file exists in the local repo
	newFile := filepath.Join(localTmpDir, "new.txt")
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("expected new.txt to exist after pull, got error: %v", err)
	}
}
