package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/snapshot"
	"github.com/zekihan/external-secrets-sops/internal/source"
	"github.com/zekihan/external-secrets-sops/internal/syncer"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

type fakeWorker struct{ triggers int }

func (f *fakeWorker) Trigger()            { f.triggers++ }
func (*fakeWorker) Status() syncer.Status { return syncer.Status{} }

type failingBroadcast struct{}

func (failingBroadcast) Broadcast(context.Context) error {
	return errors.New("private credential must not leak")
}
func (failingBroadcast) Connected() bool { return false }
func TestRouter(t *testing.T) {
	cfg := testutil.Config(t)
	store := new(snapshot.Store)
	worker := new(fakeWorker)
	s, err := snapshot.NewBuilder(cfg, testutil.Plain{}).
		Build(t.Context(), strings.Repeat("a", 40), time.Now(), []source.Document{{Project: "project", Config: "app", Environment: "dev", Encrypted: []byte("blank: ''")}, {Project: "project", Config: "empty", Environment: "dev", Encrypted: []byte("{}")}})
	if err != nil {
		t.Fatal(err)
	}
	store.Publish(s)
	api := New(
		store,
		worker,
		failingBroadcast{},
		config.Credentials{Reader: "reader", Admin: "admin"},
		time.Hour,
		testutil.Logger(),
	)
	handler := api.Handler()
	for _, tt := range []struct {
		method, path, token string
		status              int
		body                string
	}{
		{"GET", "/livez", "", 200, ""}, {"GET", "/readyz", "", 200, ""},
		{"GET", "/v1/secrets/project/app/dev/blank", "reader", 200, "{\"value\":\"\"}\n"},
		{"GET", "/v1/secrets/project/empty/dev", "reader", 200, "{\"data\":{}}\n"},
		{"GET", "/v1/secrets/project/app/typo", "reader", 404, ""},
		{"GET", "/v1/secrets/project/app/dev/missing", "reader", 404, ""},
		{"GET", "/v1/secrets/project/app/dev", "", 401, ""},
		{"GET", "/v1/secrets/project/app/dev", "bad", 401, ""},
		{"GET", "/v1/secrets/project/app/dev", "admin", 403, ""},
		{"POST", "/v1/secrets/project/app/dev", "reader", 405, ""},
		{"HEAD", "/livez", "", 405, ""},
		{"GET", "/v1/status", "reader", 403, ""}, {"GET", "/v1/status", "admin", 200, ""},
		{"GET", "/metrics", "admin", 200, ""},
		{"GET", "/v1/refresh", "admin", 405, ""}, {"POST", "/v1/refresh", "admin", 202, ""},
		{"GET", "/v1/secrets/project/../dev", "reader", 400, ""},
		{"GET", "/v1/secrets/project/app/dev/a%2Fb", "reader", 400, ""},
		{"GET", "/missing", "", 404, ""},
	} {
		t.Run(tt.method+tt.path+tt.token, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tt.status {
				t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
			}
			if tt.body != "" && rr.Body.String() != tt.body {
				t.Fatalf("body = %s", rr.Body.String())
			}
			if rr.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing cache prevention")
			}
			if strings.Contains(tt.path, "/v1/secrets/") && rr.Code == 200 &&
				rr.Header().Get("X-Secret-Revision") != s.Revision() {
				t.Fatal("missing revision")
			}
			if strings.Contains(rr.Body.String(), "private credential") {
				t.Fatal("leaked error")
			}
			if rr.Code >= 400 && !json.Valid(rr.Body.Bytes()) {
				t.Fatal("non-JSON error")
			}
		})
	}
	if worker.triggers != 1 {
		t.Fatal("broadcast failure lost local trigger")
	}
	store.Publish(s.Renew(time.Now().Add(-2 * time.Hour)))
	for _, path := range []string{"/readyz", "/v1/secrets/project/app/dev"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, http.NoBody)
		req.Header.Set("Authorization", "Bearer reader")
		handler.ServeHTTP(rr, req)
		if rr.Code != 503 {
			t.Fatal("served stale snapshot")
		}
	}
}
