package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSecret(t *testing.T) {
	setupTestConfig(t)

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/secret/project1/config1/dev",
		http.NoBody,
	)
	req.SetPathValue("project", "project1")
	req.SetPathValue("config", "config1")
	req.SetPathValue("environment", "dev")
	req.SetPathValue("key", "DATABASE_USER")

	rr := httptest.NewRecorder()

	GetSecret(rr, req)

	resp := rr.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)

	_, err := buf.ReadFrom(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	got := buf.String()

	expected := "{\"value\":\"dev_user\"}"
	if got != expected {
		t.Errorf("expected body %q, got %q", expected, got)
	}
}

func TestGetAllSecret(t *testing.T) {
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/secret/project1/config1/dev",
		http.NoBody,
	)
	req.SetPathValue("project", "project1")
	req.SetPathValue("config", "config1")
	req.SetPathValue("environment", "dev")

	rr := httptest.NewRecorder()

	GetAllSecret(rr, req)

	resp := rr.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	buf := new(bytes.Buffer)

	_, err := buf.ReadFrom(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	got := buf.String()

	expectedMap := make(map[string]map[string]string)
	expectedMapValues := make(map[string]string)
	expectedMap["values"] = expectedMapValues

	expectedMapValues["API_KEY"] = "dev_api_key_xyz789"
	expectedMapValues["BASE_URL"] = "https://api.example.com"
	expectedMapValues["DATABASE_HOST"] = "dev-db.example.com"
	expectedMapValues["DATABASE_PASSWORD"] = "dev_secret_123"
	expectedMapValues["DATABASE_PORT_UNENCRYPTED"] = "5432"
	expectedMapValues["DATABASE_USER"] = "dev_user"
	expectedMapValues["DEBUG_MODE_UNENCRYPTED"] = "true"
	expectedMapValues["JWT_SECRET"] = "dev_jwt_super_secret_key"
	expectedMapValues["NONCE_UNENCRYPTED"] = "0"
	expectedMapValues["REDIS_URL"] = "redis://dev-redis.example.com:6379"

	expected, err := json.Marshal(expectedMap)
	if err != nil {
		t.Fatalf("failed to marshal expected response: %v", err)
	}

	if got != string(expected) {
		t.Errorf("expected body %q, got %q", expected, got)
	}
}
