package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForceSync_Success(t *testing.T) {
	_, _, cleanup := setupTestRepo(t)
	t.Cleanup(cleanup)
	req := httptest.NewRequest(http.MethodPost, "/force-sync", nil)
	w := httptest.NewRecorder()

	ForceSync(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	got := buf.String()

	expected := "Synced\n"
	if got != expected {
		t.Errorf("expected body %q, got %q", expected, got)
	}
}
