package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const githubBodyLimit = 1 << 20
const githubRefreshInterval = 10 * time.Second

func (a *API) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if len(a.githubSecret) == 0 {
		a.failure(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, githubBodyLimit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			a.failure(w, http.StatusRequestEntityTooLarge, "body_too_large")
		} else {
			a.failure(w, http.StatusBadRequest, "invalid_body")
		}
		return
	}
	signature := r.Header.Get("X-Hub-Signature-256")
	digest, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	mac := hmac.New(sha256.New, a.githubSecret)
	_, _ = mac.Write(body)
	if !strings.HasPrefix(signature, "sha256=") || err != nil || !hmac.Equal(digest, mac.Sum(nil)) {
		a.failure(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	switch r.Header.Get("X-GitHub-Event") {
	case "ping":
		a.write(w, http.StatusOK, map[string]string{"message": "pong"})
	case "push":
		var payload struct {
			Ref string `json:"ref"`
		}
		if json.Unmarshal(body, &payload) != nil || payload.Ref == "" {
			a.failure(w, http.StatusBadRequest, "invalid_payload")
			return
		}
		if payload.Ref != a.githubRef {
			a.write(w, http.StatusOK, map[string]bool{"ignored": true})
			return
		}
		// Bound authenticated replay work even when delivery IDs are changed or omitted.
		a.webhookMu.Lock()
		now := time.Now()
		limited := !a.lastWebhook.IsZero() && now.Sub(a.lastWebhook) < githubRefreshInterval
		if !limited {
			a.lastWebhook = now
		}
		a.webhookMu.Unlock()
		if limited {
			a.write(w, http.StatusAccepted, map[string]bool{"coalesced": true})
			return
		}
		a.refresh(w, r)
	default:
		a.write(w, http.StatusOK, map[string]bool{"ignored": true})
	}
}
