package redis

import (
	"strconv"
	"testing"

	"github.com/zekihan/external-secrets-sops/config"
)

func TestGetRedisClient_Enabled(t *testing.T) {
	redisAddr := "localhost:" + strconv.Itoa(21331)

	err := StartMockRedisServer(t, redisAddr)
	if err != nil {
		t.Fatalf("failed to start mock redis server: %v", err)
	}

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
