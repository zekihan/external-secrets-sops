package notify

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func TestBroadcastHonorsDeadline(t *testing.T) {
	address := testutil.FreeAddress(t)
	testutil.StartRedis(t, address)
	control := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
	t.Cleanup(func() { _ = control.Close() })
	testutil.Eventually(t, func() bool { return control.Ping(t.Context()).Err() == nil })

	bus := New(
		config.Redis{Addresses: []string{address}, Channel: "test:refresh"},
		"",
		testutil.Logger(),
	)
	t.Cleanup(func() { _ = bus.Close() })
	if err := bus.Broadcast(t.Context()); err != nil {
		t.Fatalf("initial broadcast: %v", err)
	}
	if err := control.ClientPause(t.Context(), 2*time.Second).Err(); err != nil {
		t.Fatalf("pause Redis: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := bus.Broadcast(ctx); err == nil {
		t.Fatal("broadcast succeeded despite Redis pausing past the caller's deadline")
	}
}

func runBus(t *testing.T, bus *Redis) (context.CancelFunc, chan struct{}, *atomic.Int64) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var triggers atomic.Int64
	go func() {
		bus.Run(ctx, func() { triggers.Add(1) })
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not stop after cancellation")
		}
	})
	return cancel, done, &triggers
}

func TestRunTriggersOnSubscriptionAndBroadcast(t *testing.T) {
	address := testutil.FreeAddress(t)
	testutil.StartRedis(t, address)
	bus := New(
		config.Redis{Addresses: []string{address}, Channel: "test:refresh"},
		"",
		testutil.Logger(),
	)
	t.Cleanup(func() { _ = bus.Close() })
	if bus.Connected() {
		t.Fatal("connected before Run")
	}
	cancel, done, triggers := runBus(t, bus)
	// The subscription acknowledgement itself is a refresh trigger.
	testutil.Eventually(t, func() bool { return bus.Connected() && triggers.Load() >= 1 })
	before := triggers.Load()
	if err := bus.Broadcast(t.Context()); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	testutil.Eventually(t, func() bool { return triggers.Load() > before })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return on cancellation")
	}
	if bus.Connected() {
		t.Fatal("still connected after Run returned")
	}
}

func TestRunReconnectsAfterRedisRestart(t *testing.T) {
	address := testutil.FreeAddress(t)
	stop := testutil.StartRedis(t, address)
	bus := New(
		config.Redis{Addresses: []string{address}, Channel: "test:refresh"},
		"",
		testutil.Logger(),
	)
	t.Cleanup(func() { _ = bus.Close() })
	_, _, triggers := runBus(t, bus)
	testutil.Eventually(t, func() bool { return bus.Connected() })
	stop()
	testutil.Eventually(t, func() bool { return !bus.Connected() })
	before := triggers.Load()
	testutil.StartRedis(t, address)
	// Reconnect uses backoff, then the new subscription re-triggers a refresh.
	testutil.Eventually(t, func() bool { return bus.Connected() && triggers.Load() > before })
}

func TestRunWithCanceledContextReturnsImmediately(t *testing.T) {
	bus := New(
		config.Redis{Addresses: []string{testutil.FreeAddress(t)}, Channel: "test:refresh"},
		"",
		testutil.Logger(),
	)
	t.Cleanup(func() { _ = bus.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	go func() {
		bus.Run(ctx, func() { t.Error("trigger called for canceled context") })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return for canceled context")
	}
	if bus.Connected() {
		t.Fatal("connected with canceled context")
	}
}

func TestBroadcastErrorIsSanitized(t *testing.T) {
	const canary = "SECRET_CANARY_PASSWORD"
	address := testutil.FreeAddress(t) // nothing listens here
	bus := New(
		config.Redis{Addresses: []string{address}, Channel: "test:refresh"},
		canary,
		testutil.Logger(),
	)
	t.Cleanup(func() { _ = bus.Close() })
	err := bus.Broadcast(t.Context())
	if err == nil {
		t.Fatal("broadcast to unreachable Redis succeeded")
	}
	if err.Error() != "Redis broadcast failed" {
		t.Fatalf("broadcast error = %q, want the sanitized message", err)
	}
	if strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), address) {
		t.Fatalf("broadcast error leaked connection details: %q", err)
	}
}
