package redis

import (
	"strconv"
	"testing"
	"time"

	"github.com/zekihan/external-secrets-sops/config"
)

func TestGetRedisClient_Enabled(t *testing.T) {
	redisAddr := "localhost:" + strconv.Itoa(21331)
	go StartMockRedisServer(t, redisAddr)
	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond) //nolint:forbidigo // only in tests

	cfg := config.GetConfig()
	cfg.RedisConfig.Enabled = true
	cfg.RedisConfig.Addrs = []string{redisAddr}

	resetRedisClient()

	client := GetRedisClient()
	if client == nil {
		t.Error("expected redis client to be initialized when enabled")
	}
}

func TestGetRedisClient_Disabled(t *testing.T) {
	cfg := config.GetConfig()
	cfg.RedisConfig.Enabled = false

	resetRedisClient()

	client := GetRedisClient()
	if client != nil {
		t.Error("expected redis client to be nil when disabled")
	}
}
