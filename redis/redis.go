package redis

import (
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zekihan/external-secrets-sops/config"
)

var (
	redisClient redis.UniversalClient
	once        sync.Once

	defaultPoolSize        = 32                     // Default pool size for Redis connections
	defaultMaxRetries      = 3                      // Default maximum number of retries for Redis commands
	defaultMinRetryBackoff = 8 * time.Millisecond   // Default minimum retry backoff
	defaultMaxRetryBackoff = 512 * time.Millisecond // Default maximum retry backoff
)

func resetRedisClient() {
	if redisClient != nil {
		_ = redisClient.Close()
		redisClient = nil
	}

	once = sync.Once{}
}

func GetRedisClient() redis.UniversalClient {
	once.Do(func() {
		cfg := config.GetConfig()

		redisCfg := cfg.RedisConfig
		if !redisCfg.Enabled {
			return
		}

		redisClient = redis.NewUniversalClient(&redis.UniversalOptions{
			Addrs:            redisCfg.Addrs,
			ClientName:       "ESS",
			DB:               redisCfg.DB,
			Username:         redisCfg.Username,
			Password:         redisCfg.Password,
			SentinelUsername: redisCfg.SentinelUsername,
			SentinelPassword: redisCfg.SentinelPassword,
			PoolSize:         defaultPoolSize,
			MasterName:       redisCfg.MasterName,
			MaxRetries:       defaultMaxRetries,
			MinRetryBackoff:  defaultMinRetryBackoff,
			MaxRetryBackoff:  defaultMaxRetryBackoff,
		})
	})

	return redisClient
}
