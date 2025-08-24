package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForceSync_Success(t *testing.T) {
	_ = setupTestRepo(t)

	req := httptest.NewRequest(http.MethodPost, "/force-sync", http.NoBody)
	w := httptest.NewRecorder()

	ForceSync(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)

	_, err := buf.ReadFrom(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	got := buf.String()

	expected := "Sync event published successfully\n"
	if got != expected {
		t.Errorf("expected body %q, got %q", expected, got)
	}
}
