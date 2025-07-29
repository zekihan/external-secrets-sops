package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSecret(t *testing.T) {
	setupTestConfig(t)
	req := httptest.NewRequest(http.MethodGet, "/secret/project1/config1/dev", nil)
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
	buf.ReadFrom(resp.Body)
	got := buf.String()
	expected := "{\"value\":\"dev_user\"}"
	if got != expected {
		t.Errorf("expected body %q, got %q", expected, got)
	}
}

func TestGetAllSecret(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/secret/project1/config1/dev", nil)
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
	buf.ReadFrom(resp.Body)
	got := buf.String()
	expected := "{\"values\":{\"API_KEY\":\"dev_api_key_xyz789\",\"BASE_URL\":\"https://api.example.com\",\"DATABASE_HOST\":\"dev-db.example.com\",\"DATABASE_PASSWORD\":\"dev_secret_123\",\"DATABASE_PORT_UNENCRYPTED\":\"5432\",\"DATABASE_USER\":\"dev_user\",\"DEBUG_MODE_UNENCRYPTED\":\"true\",\"JWT_SECRET\":\"dev_jwt_super_secret_key\",\"NONCE_UNENCRYPTED\":\"0\",\"REDIS_URL\":\"redis://dev-redis.example.com:6379\"}}"
	if got != expected {
		t.Errorf("expected body %q, got %q", expected, got)
	}
}
