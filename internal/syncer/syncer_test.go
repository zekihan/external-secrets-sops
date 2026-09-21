package syncer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/snapshot"
	"github.com/zekihan/external-secrets-sops/internal/source"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

type fakeSource struct {
	revision string
	docs     []source.Document
	err      error
	marker   source.Marker
	fetches  atomic.Int32
}

func (f *fakeSource) Fetch(context.Context) (string, error) {
	f.fetches.Add(1)
	return f.revision, f.err
}
func (f *fakeSource) Cached() (source.Marker, error) {
	if f.marker.Revision == "" {
		return source.Marker{}, errors.New("no cache")
	}
	return f.marker, nil
}
func (f *fakeSource) Documents(context.Context, string) ([]source.Document, error) {
	return f.docs, nil
}
func (f *fakeSource) Save(r string, at time.Time) error {
	f.marker = source.Marker{Revision: r, ValidatedAt: at}
	return nil
}
func TestRefreshAndRestore(t *testing.T) {
	cfg := testutil.Config(t)
	store := new(snapshot.Store)
	src := &fakeSource{
		revision: "one",
		docs: []source.Document{
			{Project: "project", Config: "app", Environment: "dev", Encrypted: []byte("a: one")},
		},
	}
	var logs bytes.Buffer
	w := New(
		cfg.Refresh,
		src,
		snapshot.NewBuilder(cfg, testutil.Plain{}),
		store,
		slog.New(slog.NewJSONHandler(&logs, nil)),
	)
	if err := w.refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	first := store.Load()
	src.revision = "two"
	src.docs[0].Encrypted = []byte("a: [SECRET_CANARY]")
	if err := w.refresh(t.Context()); err == nil {
		t.Fatal("bad revision accepted")
	}
	if store.Load() != first {
		t.Fatal("failed revision replaced snapshot")
	}
	src.err = errors.New("SECRET_CANARY")
	if err := w.refresh(t.Context()); err == nil {
		t.Fatal("fetch failure ignored")
	}
	if bytes.Contains(logs.Bytes(), []byte("SECRET_CANARY")) {
		t.Fatal("diagnostics leak")
	}
	src.err = nil
	src.revision = "one"
	src.docs[0].Encrypted = []byte("a: [invalid]")
	if err := w.refresh(t.Context()); err != nil {
		t.Fatal("unchanged revision rebuilt")
	}
	if !store.Load().ValidatedAt().After(first.ValidatedAt()) {
		t.Fatal("freshness not renewed")
	}
	src.docs[0].Encrypted = []byte("a: one")
	restored := new(snapshot.Store)
	New(
		cfg.Refresh,
		src,
		snapshot.NewBuilder(cfg, testutil.Plain{}),
		restored,
		testutil.Logger(),
	).restore(t.Context())
	if restored.Load() == nil || restored.Load().Revision() != "one" {
		t.Fatal("cached restore failed")
	}
	src.revision = "three"
	src.docs = []source.Document{
		{Project: "project", Config: "different", Environment: "dev", Encrypted: []byte("{}")},
	}
	if err := w.refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Load().
		Values(snapshot.Key{Project: "project", Config: "app", Environment: "dev"}); ok {
		t.Fatal("deleted document retained")
	}
}

type blockingDecrypt struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (d *blockingDecrypt) Decrypt(context.Context, []byte) ([]byte, error) {
	if d.calls.Add(1) == 1 {
		close(d.started)
		<-d.release
	}
	return []byte("a: b"), nil
}
func TestCoalescingAndBlockedDeadline(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Refresh.Timeout = 20 * time.Millisecond
	cfg.Refresh.Interval = time.Hour
	d := &blockingDecrypt{started: make(chan struct{}), release: make(chan struct{})}
	src := &fakeSource{
		revision: "one",
		docs: []source.Document{
			{Project: "project", Config: "app", Environment: "dev", Encrypted: []byte("a: b")},
		},
	}
	store := new(snapshot.Store)
	w := New(cfg.Refresh, src, snapshot.NewBuilder(cfg, d), store, testutil.Logger())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	<-d.started
	for range 1000 {
		w.Trigger()
	}
	<-time.After(40 * time.Millisecond)
	if src.fetches.Load() != 1 || !w.Status().Updating {
		t.Fatal("overlapping refresh while backend blocked")
	}
	close(d.release)
	testutil.Eventually(t, func() bool { return store.Load() != nil && w.Status().Successes == 1 })
	cancel()
	<-done
	if src.fetches.Load() != 2 || w.Status().Failures != 1 {
		t.Fatalf("triggers not coalesced: %d, %+v", src.fetches.Load(), w.Status())
	}
}
func TestRetry(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Refresh.Interval = time.Hour
	cfg.Refresh.RetryMin = 5 * time.Millisecond
	cfg.Refresh.RetryMax = 10 * time.Millisecond
	src := &fakeSource{err: errors.New("offline")}
	w := New(
		cfg.Refresh,
		src,
		snapshot.NewBuilder(cfg, testutil.Plain{}),
		new(snapshot.Store),
		testutil.Logger(),
	)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	testutil.Eventually(t, func() bool { return w.Status().Failures >= 3 })
	cancel()
	<-done
}
