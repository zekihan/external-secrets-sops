package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/snapshot"
	"github.com/zekihan/external-secrets-sops/internal/syncer"
)

type Refresher interface {
	Trigger()
	Status() syncer.Status
}
type Broadcaster interface {
	Broadcast(context.Context) error
	Connected() bool
}
type API struct {
	store       *snapshot.Store
	worker      Refresher
	broadcaster Broadcaster
	maxAge      time.Duration
	reader      [32]byte
	admin       [32]byte
	logger      *slog.Logger
	httpErrors  atomic.Uint64
}

func New(
	store *snapshot.Store,
	worker Refresher,
	broadcaster Broadcaster,
	creds config.Credentials,
	maxAge time.Duration,
	logger *slog.Logger,
) *API {
	return &API{
		store:       store,
		worker:      worker,
		broadcaster: broadcaster,
		maxAge:      maxAge,
		reader:      sha256.Sum256([]byte(creds.Reader)),
		admin:       sha256.Sum256([]byte(creds.Admin)),
		logger:      logger,
	}
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(
		"/livez",
		a.method(
			http.MethodGet,
			func(w http.ResponseWriter, _ *http.Request) { a.write(w, 200, map[string]bool{"live": true}) },
		),
	)
	mux.HandleFunc("/readyz", a.method(http.MethodGet, a.ready))
	mux.HandleFunc(
		"/v1/secrets/{project}/{config}/{environment}",
		a.auth(false, a.method(http.MethodGet, a.secrets)),
	)
	mux.HandleFunc(
		"/v1/secrets/{project}/{config}/{environment}/{key}",
		a.auth(false, a.method(http.MethodGet, a.secrets)),
	)
	mux.HandleFunc("/v1/refresh", a.auth(true, a.method(http.MethodPost, a.refresh)))
	mux.HandleFunc("/v1/status", a.auth(true, a.method(http.MethodGet, a.status)))
	mux.HandleFunc("/metrics", a.auth(true, a.method(http.MethodGet, a.metrics)))
	mux.HandleFunc(
		"/",
		func(w http.ResponseWriter, _ *http.Request) { a.failure(w, 404, "not_found") },
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		defer func() {
			if recover() != nil {
				a.logger.ErrorContext(r.Context(), "HTTP handler panic")
				a.failure(w, 500, "internal_error")
			}
		}()
		// Reject unsafe decoded segments before ServeMux redirects cleaned paths.
		if strings.Contains(r.URL.Path, "\\") || strings.Contains(r.URL.Path, "//") {
			a.failure(w, 400, "invalid_path")
			return
		}
		for _, part := range strings.Split(r.URL.Path, "/") {
			if part == "." || part == ".." {
				a.failure(w, 400, "invalid_path")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *API) method(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			a.failure(w, 405, "method_not_allowed")
			return
		}
		next(w, r)
	}
}
func (a *API) auth(admin bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("Authorization")
		if !strings.HasPrefix(raw, "Bearer ") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			a.failure(w, 401, "unauthorized")
			return
		}
		sum := sha256.Sum256([]byte(strings.TrimPrefix(raw, "Bearer ")))
		readerMatch := subtle.ConstantTimeCompare(sum[:], a.reader[:])
		adminMatch := subtle.ConstantTimeCompare(sum[:], a.admin[:])
		if readerMatch|adminMatch != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			a.failure(w, 401, "unauthorized")
			return
		}
		if (admin && adminMatch != 1) || (!admin && readerMatch != 1) {
			a.failure(w, 403, "forbidden")
			return
		}
		next(w, r)
	}
}
func (a *API) write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		a.logger.Debug("HTTP response write failed")
	}
}
func (a *API) failure(w http.ResponseWriter, status int, code string) {
	a.httpErrors.Add(1)
	a.write(w, status, map[string]string{"error": code})
}
func (a *API) ready(w http.ResponseWriter, _ *http.Request) {
	if !a.store.Ready(time.Now(), a.maxAge) {
		a.failure(w, 503, "unavailable")
		return
	}
	a.write(w, 200, map[string]bool{"ready": true})
}
func (a *API) secrets(w http.ResponseWriter, r *http.Request) {
	k := snapshot.Key{
		Project:     r.PathValue("project"),
		Config:      r.PathValue("config"),
		Environment: r.PathValue("environment"),
	}
	for _, v := range []string{k.Project, k.Config, k.Environment} {
		if !config.Component(v) {
			a.failure(w, 400, "invalid_path")
			return
		}
	}
	name := r.PathValue("key")
	if name != "" && !config.Component(name) {
		a.failure(w, 400, "invalid_path")
		return
	}
	current := a.store.Load()
	if current == nil || (a.maxAge > 0 && time.Since(current.ValidatedAt()) > a.maxAge) {
		a.failure(w, 503, "unavailable")
		return
	}
	w.Header().Set("X-Secret-Revision", current.Revision())
	if name != "" {
		value, ok := current.Value(k, name)
		if !ok {
			a.failure(w, 404, "not_found")
			return
		}
		a.write(w, 200, struct {
			Value string `json:"value"`
		}{value})
		return
	}
	values, ok := current.Values(k)
	if !ok {
		a.failure(w, 404, "not_found")
		return
	}
	a.write(w, 200, struct {
		Data map[string]string `json:"data"`
	}{values})
}
func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	a.worker.Trigger()
	broadcast := "disabled"
	if a.broadcaster != nil {
		broadcast = "sent"
		if err := a.broadcaster.Broadcast(r.Context()); err != nil {
			broadcast = "failed"
		}
	}
	a.write(w, http.StatusAccepted, struct {
		Accepted  bool   `json:"accepted"`
		Broadcast string `json:"broadcast"`
	}{true, broadcast})
}
func (a *API) status(w http.ResponseWriter, _ *http.Request) {
	type status struct {
		syncer.Status
		Revision       string     `json:"revision"`
		ValidatedAt    *time.Time `json:"validatedAt"`
		AgeSeconds     float64    `json:"ageSeconds"`
		Documents      int        `json:"documents"`
		Bytes          int64      `json:"bytes"`
		Ready          bool       `json:"ready"`
		RedisEnabled   bool       `json:"redisEnabled"`
		RedisConnected bool       `json:"redisConnected"`
	}
	s := status{
		Status:       a.worker.Status(),
		Ready:        a.store.Ready(time.Now(), a.maxAge),
		RedisEnabled: a.broadcaster != nil,
	}
	if a.broadcaster != nil {
		s.RedisConnected = a.broadcaster.Connected()
	}
	if current := a.store.Load(); current != nil {
		at := current.ValidatedAt()
		s.Revision = current.Revision()
		s.ValidatedAt = &at
		s.AgeSeconds = max(0, time.Since(at).Seconds())
		s.Documents = current.Count()
		s.Bytes = current.Bytes()
	}
	a.write(w, 200, s)
}
func (a *API) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	s := a.worker.Status()
	_, _ = fmt.Fprintf(
		w,
		"# TYPE ess_refresh_total counter\ness_refresh_total{outcome=\"success\"} %d\ness_refresh_total{outcome=\"failure\"} %d\n# TYPE ess_refresh_duration_seconds summary\ness_refresh_duration_seconds_sum %g\ness_refresh_duration_seconds_count %d\n# TYPE ess_http_errors_total counter\ness_http_errors_total %d\n",
		s.Successes,
		s.Failures,
		s.TotalDurationSeconds,
		s.Successes+s.Failures,
		a.httpErrors.Load(),
	)
	connected := 0
	if a.broadcaster != nil && a.broadcaster.Connected() {
		connected = 1
	}
	_, _ = fmt.Fprintf(w, "# TYPE ess_redis_connected gauge\ness_redis_connected %d\n", connected)
	if current := a.store.Load(); current != nil {
		_, _ = fmt.Fprintf(
			w,
			"# TYPE ess_snapshot_age_seconds gauge\ness_snapshot_age_seconds %g\n# TYPE ess_snapshot_bytes gauge\ness_snapshot_bytes %d\n# TYPE ess_snapshot_documents gauge\ness_snapshot_documents %d\n# TYPE ess_snapshot_info gauge\ness_snapshot_info{revision=%q} 1\n",
			max(0, time.Since(current.ValidatedAt()).Seconds()),
			current.Bytes(),
			current.Count(),
			current.Revision(),
		)
	}
}
