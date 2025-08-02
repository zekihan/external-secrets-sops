package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-git/go-git/v5"
)

func TestGetCommit_Success(t *testing.T) {
	localTmpDir := setupTestRepo(t)

	req := httptest.NewRequest(http.MethodGet, "/commit", http.NoBody)
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

	_, err = buf.ReadFrom(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	got := buf.String()

	// Check that the response contains the commit hash or message
	if !bytes.Contains(buf.Bytes(), []byte(commit.Hash.String()[:7])) &&
		!bytes.Contains(buf.Bytes(), []byte(commit.Message)) {
		t.Errorf(
			"expected body to contain commit hash %q or message %q, got %q",
			commit.Hash.String()[:7],
			commit.Message,
			got,
		)
	}
}
