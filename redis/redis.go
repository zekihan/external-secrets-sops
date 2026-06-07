package redis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zekihan/external-secrets-sops/config"
)

var (
	redisClient redis.UniversalClient
	errRedis    error
	once        sync.Once

	errUnexpectedPong = errors.New("unexpected PONG response from Redis")

	defaultPoolSize        = 32                     // Default pool size for Redis connections
	defaultMaxRetries      = 3                      // Default maximum number of retries for Redis commands
	defaultMinRetryBackoff = 8 * time.Millisecond   // Default minimum retry backoff
	defaultMaxRetryBackoff = 512 * time.Millisecond // Default maximum retry backoff
	defaultTimeout         = 5 * time.Second        // Default timeout for Redis operations
)

func resetRedisClient() {
	if redisClient != nil {
		_ = redisClient.Close()
		redisClient = nil
	}

	errRedis = nil
	once = sync.Once{}
}

func GetRedisClient() (redis.UniversalClient, error) {
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

		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()

		res, err := redisClient.Ping(ctx).Result()
		if err != nil {
			errRedis = fmt.Errorf("failed to connect to Redis: %w", err)
			redisClient = nil

			return
		}

		if res != "PONG" {
			errRedis = fmt.Errorf("%w: %q", errUnexpectedPong, res)
			redisClient = nil

			return
		}

		slog.DebugContext(ctx, "Connected to Redis", slog.Any("addrs", redisCfg.Addrs))
	})

	return redisClient, errRedis
}
