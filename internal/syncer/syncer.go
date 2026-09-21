package syncer

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/snapshot"
	"github.com/zekihan/external-secrets-sops/internal/source"
)

type Source interface {
	Fetch(context.Context) (string, error)
	Cached() (source.Marker, error)
	Documents(context.Context, string) ([]source.Document, error)
	Save(string, time.Time) error
}
type Builder interface {
	Build(context.Context, string, time.Time, []source.Document) (*snapshot.Snapshot, error)
}
type Status struct {
	Phase                string    `json:"phase"`
	Updating             bool      `json:"updating"`
	LastAttempt          time.Time `json:"lastAttempt"`
	LastError            string    `json:"lastError"`
	Successes            uint64    `json:"successes"`
	Failures             uint64    `json:"failures"`
	DurationSeconds      float64   `json:"durationSeconds"`
	TotalDurationSeconds float64   `json:"totalDurationSeconds"`
}
type Worker struct {
	cfg      config.Refresh
	source   Source
	builder  Builder
	store    *snapshot.Store
	logger   *slog.Logger
	triggers chan struct{}
	mu       sync.RWMutex
	status   Status
}

func New(
	cfg config.Refresh,
	src Source,
	builder Builder,
	store *snapshot.Store,
	logger *slog.Logger,
) *Worker {
	return &Worker{
		cfg:      cfg,
		source:   src,
		builder:  builder,
		store:    store,
		logger:   logger,
		triggers: make(chan struct{}, 1),
	}
}
func (w *Worker) Trigger() {
	select {
	case w.triggers <- struct{}{}:
	default:
	}
}
func (w *Worker) Status() Status { w.mu.RLock(); defer w.mu.RUnlock(); return w.status }
func (w *Worker) setPhase(phase string) {
	w.mu.Lock()
	w.status.Phase = phase
	w.mu.Unlock()
}
func (w *Worker) restore(ctx context.Context) {
	w.mu.Lock()
	w.status.Updating = true
	w.status.Phase = "restoring"
	w.status.LastAttempt = time.Now()
	w.mu.Unlock()
	defer func() { w.mu.Lock(); w.status.Updating = false; w.status.Phase = "idle"; w.mu.Unlock() }()

	marker, err := w.source.Cached()
	if err != nil {
		return
	}
	attempt, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()
	docs, err := w.source.Documents(attempt, marker.Revision)
	if err != nil {
		return
	}
	candidate, err := w.builder.Build(attempt, marker.Revision, marker.ValidatedAt, docs)
	if err == nil && attempt.Err() == nil {
		w.store.Publish(candidate)
		w.logger.InfoContext(
			ctx,
			"Restored validated snapshot",
			slog.String("revision", marker.Revision),
		)
	}
}
func (w *Worker) refresh(ctx context.Context) (err error) {
	start := time.Now()
	failureCode := "fetch_failed"
	attempt, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()
	w.mu.Lock()
	w.status.Updating = true
	w.status.Phase = "fetching"
	w.status.LastAttempt = start
	w.mu.Unlock()
	defer func() {
		duration := time.Since(start).Seconds()
		if attempt.Err() != nil {
			failureCode = "deadline_exceeded"
		}
		w.mu.Lock()
		w.status.Updating = false
		w.status.Phase = "idle"
		w.status.DurationSeconds = duration
		w.status.TotalDurationSeconds += duration
		if err != nil {
			w.status.Failures++
			w.status.LastError = failureCode
		} else {
			w.status.Successes++
			w.status.LastError = ""
		}
		w.mu.Unlock()
		if err != nil {
			w.logger.WarnContext(
				ctx,
				"Snapshot refresh failed",
				slog.String("code", failureCode),
			)
		}
	}()
	revision, err := w.source.Fetch(attempt)
	if err != nil {
		return err
	}
	at := time.Now()
	current := w.store.Load()
	var candidate *snapshot.Snapshot
	if current != nil && current.Revision() == revision {
		candidate = current.Renew(at)
	} else {
		w.setPhase("reading")
		failureCode = "read_failed"
		docs, e := w.source.Documents(attempt, revision)
		if e != nil {
			return e
		}
		w.setPhase("decrypting")
		failureCode = "validation_failed"
		candidate, e = w.builder.Build(attempt, revision, at, docs)
		if e != nil {
			return e
		}
	}
	if err = attempt.Err(); err != nil {
		return err
	}
	w.setPhase("persisting")
	failureCode = "cache_write_failed"
	if err = w.source.Save(revision, at); err != nil {
		return err
	}
	if err = attempt.Err(); err != nil {
		return err
	}
	w.store.Publish(candidate)
	w.logger.InfoContext(
		ctx,
		"Validated snapshot",
		slog.String("revision", revision),
		slog.Int("documents", candidate.Count()),
	)
	return nil
}
func jitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.9 + rand.Float64()*0.2))
}
func (w *Worker) Run(ctx context.Context) {
	w.restore(ctx)
	w.Trigger()
	timer := time.NewTimer(jitter(w.cfg.Interval))
	defer timer.Stop()
	retry := w.cfg.RetryMin
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-w.triggers:
		}
		if ctx.Err() != nil {
			return
		}
		err := w.refresh(ctx)
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return
		}
		next := jitter(w.cfg.Interval)
		if err != nil {
			next = jitter(retry)
			retry = min(retry*2, w.cfg.RetryMax)
		} else {
			retry = w.cfg.RetryMin
		}
		timer.Reset(next)
	}
}
