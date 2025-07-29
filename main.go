package main

import (
	"fmt"
	"github.com/justinas/alice"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/external-secrets-sops/config"
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/external-secrets-sops/routes/api"
	"github.com/zekihan/go-commons/environment"
	"github.com/zekihan/go-commons/middleware"
	"github.com/zekihan/go-commons/utils"
	"github.com/zekihan/go-commons/web/errorroutes"
	"github.com/zekihan/go-commons/web/health"
	"github.com/zekihan/go-commons/web/ping"
	"net/http"
	"os"
)

var stdChain alice.Chain
var cfg *config.Config

func init() {
	err := environment.LoadEnvironmentFile()
	if err != nil {
		log.Errorf("Error loading environment file: %v", err)
		os.Exit(1)
	}

	// init config
	cfg = config.GetConfig()

	// init logger
	err = utils.InitLogger(&utils.LoggerConfig{
		Path:          cfg.LogConfig.Path,
		Level:         cfg.LogConfig.Level,
		AddMethodName: cfg.LogConfig.AddMethodName,
	})
	if err != nil {
		log.Errorf("Error initializing logger: %v", err)
		os.Exit(1)
	}

	// init middleware
	stdChain = alice.New(middleware.RecoverHandler, middleware.Logging)
}

func main() {
	log.Info("Starting external secrets sops server")
	log.Info(utils.GetStartupInfo())

	go func() {
		if cfg.GitConfig.RepoURL != "" {
			git.StartPullLoop()
		} else {
			log.Warn("Git repository URL is not configured, skipping git pulling")
		}
	}()

	// API routes
	http.HandleFunc("/api/ping", ping.GetPing)
	http.HandleFunc("/api/health", health.GetHealth)
	http.HandleFunc("/api/ready", api.GetReady)

	// git routes
	http.HandleFunc("/git/lastCommit", api.GetCommit)
	http.HandleFunc("/git/forceSync", api.ForceSync)

	// Secret routes
	http.HandleFunc("/secret/{project}/{config}/{environment}", api.GetAllSecret)
	http.HandleFunc("/secret/{project}/{config}/{environment}/{key}", api.GetSecret)

	// Error routes
	http.HandleFunc("/404", errorroutes.GetNotFound)
	http.HandleFunc("/5xx", errorroutes.Get5xx)

	// Redirect all other paths
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Redirect root path to dashboard
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/dashboard", http.StatusMovedPermanently)
			return
		}
		// Redirect all other paths to 404
		http.Redirect(w, r, "/404", http.StatusMovedPermanently)
	})

	port := cfg.WebConfig.Port
	host := cfg.WebConfig.Host
	log.Infof("Starting server on %s:%d", host, port)
	err := http.ListenAndServe(fmt.Sprintf("%s:%d", host, port), stdChain.Then(http.DefaultServeMux))
	if err != nil {
		log.Fatal(err)
	}

	git.StopPullLoop()
}
