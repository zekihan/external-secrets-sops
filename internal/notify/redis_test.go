package notify

import (
	"context"
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
