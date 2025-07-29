package api

import (
	"bytes"
	"github.com/go-git/go-git/v5"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetCommit_Success(t *testing.T) {
	_, localTmpDir, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)
	req := httptest.NewRequest(http.MethodGet, "/commit", nil)
	w := httptest.NewRecorder()

	GetCommit(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// Get the latest commit hash from the local repo
	repo, err := git.PlainOpen(localTmpDir)
	if err != nil {
		t.Fatalf("failed to open repo: %v", err)
	}
	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("failed to get HEAD: %v", err)
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("failed to get commit: %v", err)
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	got := buf.String()

	// Check that the response contains the commit hash or message
	if !bytes.Contains(buf.Bytes(), []byte(commit.Hash.String()[:7])) && !bytes.Contains(buf.Bytes(), []byte(commit.Message)) {
		t.Errorf("expected body to contain commit hash %q or message %q, got %q", commit.Hash.String()[:7], commit.Message, got)
	}
}
