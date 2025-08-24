package main

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/justinas/alice"
	"github.com/zekihan/go-commons/environment"
	"github.com/zekihan/go-commons/middleware"
	"github.com/zekihan/go-commons/utils"
	"github.com/zekihan/go-commons/web/errorroutes"
	"github.com/zekihan/go-commons/web/health"
	"github.com/zekihan/go-commons/web/ping"

	log "github.com/sirupsen/logrus"

	"github.com/zekihan/external-secrets-sops/config"
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/external-secrets-sops/pubsub"
	"github.com/zekihan/external-secrets-sops/routes/api"
)

const (
	defaultTimeout = 10 * time.Second
)

var (
	stdChain, authChain alice.Chain
	cfg                 *config.Config
)

func init() {
	err := environment.LoadEnvironmentFile()
	if err != nil {
		log.Errorf("Error loading environment file: %v", err)
		os.Exit(1)
	}

	sopsAgeKeyFile := os.Getenv("SOPS_AGE_KEY_FILE")
	if sopsAgeKeyFile != "" {
		sopsAgeKeyFile = strings.Replace(sopsAgeKeyFile, "~", os.Getenv("HOME"), 1)

		err = os.Setenv("SOPS_AGE_KEY_FILE", sopsAgeKeyFile)
		if err != nil {
			log.Errorf("Error setting SOPS_AGE_KEY_FILE environment variable: %v", err)
			os.Exit(1)
		}
	}

	// Init config.
	cfg = config.GetConfig()

	// Init logger.
	err = utils.InitLogger(&utils.LoggerConfig{
		Path:          cfg.LogConfig.Path,
		Level:         cfg.LogConfig.Level,
		AddMethodName: cfg.LogConfig.AddMethodName,
	})
	if err != nil {
		log.Errorf("Error initializing logger: %v", err)
		os.Exit(1)
	}

	// Init middleware.
	stdChain = alice.New(middleware.RecoverHandler, middleware.Logging)
	authChain = alice.New(
		middleware.GetAuthMiddlewareForSingleAPIKey(config.GetConfig().WebConfig.APIKey),
	)
}

func main() {
	log.Info("Starting external secrets sops server")
	log.Info(utils.GetStartupInfo())

	cancelListenGithubEvents := pubsub.ListenGithubEvents()
	defer cancelListenGithubEvents()

	if cfg.GitConfig.RepoURL != "" {
		cancelGitPullLoop := git.StartPullLoop()
		defer cancelGitPullLoop()
	} else {
		log.Warn("Git repository URL is not configured, skipping git pulling")
	}

	runWebServer()
}

func runWebServer() {
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
	log.Infof("Starting server on http://%s", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      stdChain.Then(http.DefaultServeMux),
		ReadTimeout:  defaultTimeout,
		WriteTimeout: defaultTimeout,
	}

	err := srv.ListenAndServe()
	if err != nil {
		log.Errorf("Failed to start server: %v", err)
		os.Exit(1)
	}
}
