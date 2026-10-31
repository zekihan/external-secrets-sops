package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/decrypt"
	"github.com/zekihan/external-secrets-sops/internal/httpapi"
	"github.com/zekihan/external-secrets-sops/internal/notify"
	"github.com/zekihan/external-secrets-sops/internal/snapshot"
	"github.com/zekihan/external-secrets-sops/internal/source"
	"github.com/zekihan/external-secrets-sops/internal/syncer"
)

func Run(
	ctx context.Context,
	cfg config.Config,
	creds config.Credentials,
	logger *slog.Logger,
) error {
	repository, err := source.New(cfg, creds.Git)
	if err != nil {
		return err
	}
	listener, err := listen(cfg.Server)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	store := new(snapshot.Store)
	worker := syncer.New(
		cfg.Refresh,
		repository,
		snapshot.NewBuilder(cfg, decrypt.SOPS{}),
		store,
		logger,
	)
	var broadcaster httpapi.Broadcaster
	var notifications *notify.Redis
	var wg sync.WaitGroup
	wg.Go(func() { worker.Run(ctx) })
	if cfg.Redis.Enabled {
		notifications = notify.New(cfg.Redis, creds.Redis, logger)
		defer func() { _ = notifications.Close() }()
		broadcaster = notifications
		wg.Go(func() { notifications.Run(ctx, worker.Trigger) })
	}
	api := httpapi.New(
		store,
		worker,
		broadcaster,
		creds,
		cfg.Git.Branch,
		cfg.Refresh.MaxStaleness,
		logger,
	)
	server := &http.Server{
		Handler:           api.Handler(),
		ErrorLog:          serverErrorLog(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	logger.InfoContext(ctx, "HTTP server started", slog.String("address", listener.Addr().String()))
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-result:
	}
	cancel()
	drain(server, &wg, cfg.Server.ShutdownTimeout, logger)
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("HTTP server failed")
	}
	return nil
}

func drain(server *http.Server, wg *sync.WaitGroup, timeout time.Duration, logger *slog.Logger) {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		logger.WarnContext(shutdownCtx, "Shutdown deadline reached")
	}
}
