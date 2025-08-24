package redis

import (
	"testing"

	"github.com/zekihan/external-secrets-sops/config"
)

func TestGetRedisClient_Enabled(t *testing.T) {
	cfg := config.GetConfig()
	cfg.RedisConfig.Enabled = true

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
