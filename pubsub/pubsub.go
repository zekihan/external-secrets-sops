package pubsub

import (
	"context"
	"log/slog"
	"sync"

	"github.com/zekihan/go-commons/pubsub"

	"github.com/zekihan/external-secrets-sops/config"
	"github.com/zekihan/external-secrets-sops/redis"
)

var (
	pubSuber     *pubsub.UniversalPubSub
	pubSuberOnce sync.Once
)

func getPubSuber() *pubsub.UniversalPubSub {
	pubSuberOnce.Do(func() {
		opts := new(pubsub.UniversalPubSubOptions)
		opts.Prefix = "ess"

		if config.GetConfig().RedisConfig.Enabled {
			redisClient, err := redis.GetRedisClient()
			if err != nil {
				slog.ErrorContext(
					context.Background(),
					"Redis unavailable, falling back to in-process pubsub",
					slog.Any("error", err),
				)
			} else {
				opts.Redis = redisClient
			}
		}

		pubSuber = pubsub.NewUniversalPubSub(opts)
	})

	return pubSuber
}
