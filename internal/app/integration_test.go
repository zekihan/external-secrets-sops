package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/decrypt"
	"github.com/zekihan/external-secrets-sops/internal/httpapi"
	"github.com/zekihan/external-secrets-sops/internal/notify"
	"github.com/zekihan/external-secrets-sops/internal/snapshot"
	"github.com/zekihan/external-secrets-sops/internal/source"
	"github.com/zekihan/external-secrets-sops/internal/syncer"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func TestTwoReplicaRedisRecovery(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Refresh.Interval = time.Hour
	cfg.Refresh.RetryMin = 20 * time.Millisecond
	cfg.Refresh.RetryMax = 50 * time.Millisecond
	remote := testutil.InitRepo(t, cfg.Git.URL, "main")
	path := "generated/project/app/dev/values.yaml"
	first := testutil.Commit(
		t,
		remote,
		map[string][]byte{path: testutil.Encrypt(t, "value: first")},
	)
	address := testutil.FreeAddress(t)
	cfg.Redis = config.Redis{Enabled: true, Addresses: []string{address}, Channel: "test:refresh"}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stores := []*snapshot.Store{new(snapshot.Store), new(snapshot.Store)}
	buses := make([]*notify.Redis, 2)
	servers := make([]*httptest.Server, 2)
	var dones []chan struct{}
	for i := range 2 {
		instance := cfg
		instance.Git.CacheDir = t.TempDir()
		repo, err := source.New(instance, "")
		if err != nil {
			t.Fatal(err)
		}
		worker := syncer.New(
			cfg.Refresh,
			repo,
			snapshot.NewBuilder(instance, decrypt.SOPS{}),
			stores[i],
			testutil.Logger(),
		)
		bus := notify.New(cfg.Redis, "", testutil.Logger())
		buses[i] = bus
		t.Cleanup(func() { _ = bus.Close() })
		done := make(chan struct{})
		dones = append(dones, done)
		go func() { worker.Run(ctx); close(done) }()
		go bus.Run(ctx, worker.Trigger)
		servers[i] = httptest.NewServer(
			httpapi.New(stores[i], worker, bus, config.Credentials{Reader: "reader", Admin: "admin"}, 0, testutil.Logger()).
				Handler(),
		)
		t.Cleanup(servers[i].Close)
	}
	both := func(rev string) bool {
		for _, store := range stores {
			if store.Load() == nil || store.Load().Revision() != rev {
				return false
			}
		}
		return true
	}
	testutil.Eventually(t, func() bool { return both(first) })
	// Git startup is independent of Redis availability.
	stop := testutil.StartRedis(t, address)
	testutil.Eventually(t, func() bool { return buses[0].Connected() && buses[1].Connected() })
	second := testutil.Commit(
		t,
		remote,
		map[string][]byte{path: testutil.Encrypt(t, "value: second")},
	)
	req, _ := http.NewRequestWithContext(
		t.Context(),
		"POST",
		servers[0].URL+"/v1/refresh",
		http.NoBody,
	)
	req.Header.Set("Authorization", "Bearer admin")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 202 {
		t.Fatalf("refresh: %s", body)
	}
	testutil.Eventually(t, func() bool { return both(second) })
	req, _ = http.NewRequestWithContext(
		t.Context(),
		"GET",
		servers[1].URL+"/v1/secrets/project/app/dev",
		http.NoBody,
	)
	req.Header.Set("Authorization", "Bearer reader")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Data map[string]string `json:"data"`
	}
	err = json.NewDecoder(response.Body).Decode(&data)
	response.Body.Close()
	if err != nil || data.Data["value"] != "second" ||
		response.Header.Get("X-Secret-Revision") != second {
		t.Fatal("replica response mismatch")
	}
	stop()
	testutil.Eventually(t, func() bool { return !buses[0].Connected() && !buses[1].Connected() })
	third := testutil.Commit(
		t,
		remote,
		map[string][]byte{path: testutil.Encrypt(t, "value: third")},
	)
	testutil.StartRedis(t, address)
	testutil.Eventually(t, func() bool { return both(third) })
	testutil.Commit(t, remote, map[string][]byte{path: []byte("unencrypted: secret-canary")})
	if err = buses[0].Broadcast(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Polling the exposed status observes rejection without timing assumptions.
	testutil.Eventually(t, func() bool {
		r, _ := http.NewRequestWithContext(
			t.Context(),
			"GET",
			servers[0].URL+"/v1/status",
			http.NoBody,
		)
		r.Header.Set("Authorization", "Bearer admin")
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			return false
		}
		defer resp.Body.Close()
		var status struct {
			Failures uint64 `json:"failures"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&status)
		return status.Failures > 0
	})
	if !both(third) {
		t.Fatal("invalid candidate replaced valid data")
	}
	cancel()
	for _, done := range dones {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("worker did not stop")
		}
	}
}
func TestShutdownWithoutGit(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Server.Address = "127.0.0.1:0"
	cfg.Server.ShutdownTimeout = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, config.Credentials{Reader: "reader", Admin: "admin"}, testutil.Logger()) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
}

func TestShutdownWithUncancellableBackend(t *testing.T) {
	var wg sync.WaitGroup
	release := make(chan struct{})
	wg.Go(func() { <-release })
	started := time.Now()
	drain(&http.Server{}, &wg, 20*time.Millisecond, testutil.Logger())
	close(release)
	wg.Wait()
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond || elapsed > time.Second {
		t.Fatalf("shutdown duration %v", elapsed)
	}
}

func TestRestoreEncryptedCacheWhileGitOffline(t *testing.T) {
	cfg := testutil.Config(t)
	remote := testutil.InitRepo(t, cfg.Git.URL, "main")
	revision := testutil.Commit(
		t,
		remote,
		map[string][]byte{
			"generated/project/app/dev/values.yaml": testutil.Encrypt(
				t,
				"value: CACHE_PLAINTEXT_CANARY",
			),
		},
	)
	repository, err := source.New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	docs, err := repository.Documents(t.Context(), revision)
	if err != nil {
		t.Fatal(err)
	}
	builder := snapshot.NewBuilder(cfg, decrypt.SOPS{})
	if _, err = builder.Build(t.Context(), revision, time.Now(), docs); err != nil {
		t.Fatal(err)
	}
	if err = repository.Save(revision, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(cfg.Git.URL, cfg.Git.URL+"-offline"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(cfg.Git.URL+"-offline", cfg.Git.URL) })
	restarted, err := source.New(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	store := new(snapshot.Store)
	worker := syncer.New(cfg.Refresh, restarted, builder, store, testutil.Logger())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { worker.Run(ctx); close(done) }()
	testutil.Eventually(
		t,
		func() bool { return store.Load() != nil && worker.Status().Failures > 0 },
	)
	if store.Load().Revision() != revision {
		t.Fatal("wrong restored revision")
	}
	if value, _ := store.Load().
		Value(snapshot.Key{Project: "project", Config: "app", Environment: "dev"}, "value"); value != "CACHE_PLAINTEXT_CANARY" {
		t.Fatal("cached decrypt failed")
	}
	cancel()
	<-done
	err = filepath.WalkDir(
		cfg.Git.CacheDir,
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			if bytes.Contains(data, []byte("CACHE_PLAINTEXT_CANARY")) {
				t.Error("cache contains plaintext")
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}
