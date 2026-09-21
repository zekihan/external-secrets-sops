package notify

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zekihan/external-secrets-sops/internal/config"
)

type Redis struct {
	client    redis.UniversalClient
	channel   string
	logger    *slog.Logger
	connected atomic.Bool
}

func New(cfg config.Redis, password string, logger *slog.Logger) *Redis {
	return &Redis{client: redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:        cfg.Addresses,
		Username:     cfg.Username,
		Password:     password,
		MasterName:   cfg.MasterName,
		DB:           cfg.DB,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		MaxRetries:   -1,
	}), channel: cfg.Channel, logger: logger}
}
func (r *Redis) Connected() bool { return r.connected.Load() }
func (r *Redis) Close() error    { return r.client.Close() }
func (r *Redis) Broadcast(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := r.client.Publish(ctx, r.channel, "refresh").Err(); err != nil {
		return errors.New("Redis broadcast failed")
	}
	return nil
}
func (r *Redis) Run(ctx context.Context, trigger func()) {
	delay := time.Second
	for ctx.Err() == nil {
		sub := r.client.Subscribe(ctx, r.channel)
		connectionCtx, stop := context.WithCancel(ctx)
		stopClose := context.AfterFunc(connectionCtx, func() { _ = sub.Close() })
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-connectionCtx.Done():
					return
				case <-ticker.C:
					pingCtx, cancel := context.WithTimeout(connectionCtx, 3*time.Second)
					err := sub.Ping(pingCtx)
					cancel()
					if err != nil {
						_ = sub.Close()
						return
					}
				}
			}
		}()
		// Receive exposes subscription acknowledgements, including reconnections.
		for ctx.Err() == nil {
			receiveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			event, err := sub.ReceiveTimeout(receiveCtx, 30*time.Second)
			cancel()
			if err != nil {
				break
			}
			switch event.(type) {
			case *redis.Subscription:
				r.connected.Store(true)
				delay = time.Second
				trigger()
			case *redis.Message:
				trigger()
			}
		}
		r.connected.Store(false)
		stop()
		stopClose()
		_ = sub.Close()
		if ctx.Err() != nil {
			return
		}
		r.logger.DebugContext(ctx, "Redis subscription reconnecting")
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}
