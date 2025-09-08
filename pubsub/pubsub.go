package pubsub

import (
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
		opts := &pubsub.UniversalPubSubOptions{
			Prefix: "ess",
		}

		if config.GetConfig().RedisConfig.Enabled {
			opts.Redis = redis.GetRedisClient()
		}

		pubSuber = pubsub.NewUniversalPubSub(opts)
	})

	return pubSuber
}
