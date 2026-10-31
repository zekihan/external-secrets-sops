package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/snapshot"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func githubSignature(body, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func githubRequest(event, body, signature string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Delivery", "same-delivery")
	return req
}

func TestGitHubWebhook(t *testing.T) {
	const secret = "synthetic-webhook-secret"
	const push = `{"ref":"refs/heads/release/stable"}`
	for _, tt := range []struct {
		name, event, body, secret, signature string
		status, triggers                     int
	}{
		{"valid push", "push", push, secret, githubSignature(push, secret), 202, 1},
		{"invalid signature", "push", push, secret, githubSignature(push, "wrong"), 401, 0},
		{"missing signature", "push", push, secret, "", 401, 0},
		{"malformed signature", "push", push, secret, "sha256=zz", 401, 0},
		{"wrong algorithm", "push", push, secret, "sha1=abc", 401, 0},
		{"missing prefix", "push", push, secret, strings.TrimPrefix(githubSignature(push, secret), "sha256="), 401, 0},
		{"short signature", "push", push, secret, "sha256=00", 401, 0},
		{"tampered body", "push", push + " ", secret, githubSignature(push, secret), 401, 0},
		{"unset secret", "push", push, "", githubSignature(push, ""), 401, 0},
		{"unset unsigned", "ping", "{}", "", "", 401, 0},
		{"ping", "ping", "{}", secret, githubSignature("{}", secret), 200, 0},
		{"other event", "issues", "{}", secret, githubSignature("{}", secret), 200, 0},
		{"unsigned other event", "issues", "{}", secret, "", 401, 0},
		{"other branch", "push", `{"ref":"refs/heads/main"}`, secret, githubSignature(`{"ref":"refs/heads/main"}`, secret), 200, 0},
		{"tag push", "push", `{"ref":"refs/tags/release/stable"}`, secret, githubSignature(`{"ref":"refs/tags/release/stable"}`, secret), 200, 0},
		{"malformed payload", "push", "{", secret, githubSignature("{", secret), 400, 0},
		{"missing ref", "push", "{}", secret, githubSignature("{}", secret), 400, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			worker := new(fakeWorker)
			api := New(
				new(snapshot.Store),
				worker,
				nil,
				config.Credentials{GitHubWebhookSecret: tt.secret},
				"release/stable",
				0,
				testutil.Logger(),
			)
			rr := httptest.NewRecorder()
			api.Handler().ServeHTTP(rr, githubRequest(tt.event, tt.body, tt.signature))
			if rr.Code != tt.status || worker.triggers != tt.triggers {
				t.Fatalf(
					"status=%d triggers=%d, want %d/%d",
					rr.Code,
					worker.triggers,
					tt.status,
					tt.triggers,
				)
			}
			if tt.event == "ping" && rr.Code == 200 && !strings.Contains(rr.Body.String(), "pong") {
				t.Fatal("missing pong")
			}
		})
	}
}

func TestGitHubBodyLimit(t *testing.T) {
	for _, n := range []int{githubBodyLimit, githubBodyLimit + 1} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			body := strings.Repeat(" ", n-2) + "{}"
			api := New(
				new(snapshot.Store),
				new(fakeWorker),
				nil,
				config.Credentials{GitHubWebhookSecret: "fixture"},
				"main",
				0,
				testutil.Logger(),
			)
			req := githubRequest("ping", body, githubSignature(body, "fixture"))
			// Exercise the streaming limit rather than trusting Content-Length.
			req.ContentLength = -1
			rr := httptest.NewRecorder()
			api.Handler().ServeHTTP(rr, req)
			want := 200
			if n > githubBodyLimit {
				want = 413
			}
			if rr.Code != want {
				t.Fatalf("status=%d, want %d", rr.Code, want)
			}
		})
	}
}

type countingBroadcast struct{ calls atomic.Int64 }

func (b *countingBroadcast) Broadcast(context.Context) error { b.calls.Add(1); return nil }
func (*countingBroadcast) Connected() bool                   { return true }

func TestGitHubReplayInterval(t *testing.T) {
	worker := new(fakeWorker)
	bus := new(countingBroadcast)
	api := New(
		new(snapshot.Store),
		worker,
		bus,
		config.Credentials{GitHubWebhookSecret: "fixture"},
		"main",
		0,
		testutil.Logger(),
	)
	handler := api.Handler()
	body := `{"ref":"refs/heads/main"}`
	send := func(delivery string) {
		req := githubRequest("push", body, githubSignature(body, "fixture"))
		req.Header.Set("X-GitHub-Delivery", delivery)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != 202 {
			t.Errorf("status=%d", rr.Code)
		}
	}
	send("same")
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() { send([]string{"same", "different", ""}[i%3]) })
	}
	wg.Wait()
	if worker.triggers != 1 || bus.calls.Load() != 1 {
		t.Fatal("replays triggered work")
	}
	api.lastWebhook = time.Now().Add(-githubRefreshInterval)
	send("same")
	if worker.triggers != 2 || bus.calls.Load() != 2 {
		t.Fatal("interval expiry suppressed refresh")
	}
}

func TestGitHubBroadcastFailure(t *testing.T) {
	worker := new(fakeWorker)
	api := New(
		new(snapshot.Store),
		worker,
		failingBroadcast{},
		config.Credentials{GitHubWebhookSecret: "fixture"},
		"main",
		0,
		testutil.Logger(),
	)
	body := `{"ref":"refs/heads/main"}`
	rr := httptest.NewRecorder()
	api.Handler().ServeHTTP(rr, githubRequest("push", body, githubSignature(body, "fixture")))
	if rr.Code != 202 || worker.triggers != 1 ||
		!strings.Contains(rr.Body.String(), `"broadcast":"failed"`) {
		t.Fatal("broadcast failure lost accepted refresh")
	}
}
