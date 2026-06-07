package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/justinas/alice"
	"github.com/zekihan/go-commons/environment"
	"github.com/zekihan/go-commons/middleware"
	"github.com/zekihan/go-commons/utils"
	"github.com/zekihan/go-commons/web/errorroutes"
	"github.com/zekihan/go-commons/web/health"
	"github.com/zekihan/go-commons/web/ping"

	"github.com/zekihan/external-secrets-sops/config"
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/external-secrets-sops/pubsub"
	"github.com/zekihan/external-secrets-sops/routes/api"
)

const (
	defaultTimeout  = 10 * time.Second
	shutdownTimeout = 30 * time.Second
)

var (
	stdChain, authChain alice.Chain
	cfg                 *config.Config
)

func init() {
	err := environment.LoadEnvironmentFile()
	if err != nil {
		slog.ErrorContext(
			context.Background(),
			"Failed to load environment file",
			slog.Any("error", err),
		)
		os.Exit(1)
	}

	sopsAgeKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
	if sopsAgeKeyFile != "" {
		sopsAgeKeyFile = strings.Replace(sopsAgeKeyFile, "~", os.Getenv("HOME"), 1)

		err = os.Setenv("SOPS_AGE_KEY_FILE", sopsAgeKeyFile)
		if err != nil {
			slog.ErrorContext(
				context.Background(),
				"Error setting SOPS_AGE_KEY_FILE environment variable",
				slog.Any("error", err),
			)
			os.Exit(1)
		}
	}

	// Init config.
	cfg = config.GetConfig()

	// Init logger.
	utils.InitLogger(&utils.LoggerConfig{
		Level:     cfg.LogConfig.Level,
		AddSource: cfg.LogConfig.AddSource,
	})

	// Init middleware.
	stdChain = alice.New(middleware.RecoverHandler, middleware.Logging)
	authChain = alice.New(
		middleware.GetAuthMiddlewareForSingleAPIKey(config.GetConfig().WebConfig.APIKey),
	)
}

func main() {
	os.Exit(run())
}

func run() int {
	slog.InfoContext(context.Background(), "Starting external secrets sops server")
	utils.LogVersionInfo(context.Background())

	cancelListenGithubEvents := pubsub.ListenGithubEvents()
	defer cancelListenGithubEvents()

	if cfg.GitConfig.RepoURL != "" {
		cancelGitPullLoop := git.StartPullLoop()
		defer cancelGitPullLoop()
	} else {
		slog.WarnContext(
			context.Background(),
			"Git repository URL is not configured, skipping git pulling",
		)
	}

	err := runWebServer()
	if err != nil {
		slog.ErrorContext(context.Background(), "Server error", slog.Any("error", err))

		return 1
	}

	return 0
}

func runWebServer() error {
	// API routes.
	http.HandleFunc("/api/ping", ping.GetPing)
	http.HandleFunc("/api/health", health.GetHealth)
	http.HandleFunc("/api/ready", api.GetReady)

	// Git routes.
	http.Handle("/git/lastCommit", authChain.ThenFunc(api.GetCommit))
	http.Handle("/git/forceSync", authChain.ThenFunc(api.ForceSync))

	// Secret routes.
	http.Handle("/secret/{project}/{config}/{environment}", authChain.ThenFunc(api.GetAllSecret))
	http.Handle("/secret/{project}/{config}/{environment}/{key}", authChain.ThenFunc(api.GetSecret))

	// Error routes.
	http.HandleFunc("/404", errorroutes.GetNotFound)
	http.HandleFunc("/5xx", errorroutes.Get5xx)

	// Redirect all other paths.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Redirect all other paths to 404.
		http.Redirect(w, r, "/404", http.StatusMovedPermanently)
	})

	port := config.GetConfig().WebConfig.Port
	host := config.GetConfig().WebConfig.Host
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	slog.InfoContext(context.Background(), "Starting server on http://"+addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      stdChain.Then(http.DefaultServeMux),
		ReadTimeout:  defaultTimeout,
		WriteTimeout: defaultTimeout,
	}

	serverErr := make(chan error, 1)

	go func() {
		err := srv.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		} else {
			serverErr <- nil
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return err
	case <-sigChan:
		slog.InfoContext(context.Background(), "Shutdown signal received, stopping server")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	err := srv.Shutdown(shutdownCtx)
	if err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	return nil
}
