package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReady_Success(t *testing.T) {
	_, _, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)
	req := httptest.NewRequest(http.MethodPost, "/ready", nil)
	w := httptest.NewRecorder()

	GetReady(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	got := buf.String()

	expected := "OK\n"
	if got != expected {
		t.Errorf("expected body %q, got %q", expected, got)
	}
}

// TestReady_NotReady tests the case when the repo is not ready.
func TestReady_NotReady(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/ready", nil)
	w := httptest.NewRecorder()

	GetReady(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	got := buf.String()

	expected := "Service is not ready\n"
	if got != expected {
		t.Errorf("expected body %q, got %q", expected, got)
	}
}
