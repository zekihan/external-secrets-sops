package main

import (
	"fmt"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/external-secrets-sops/config"
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/external-secrets-sops/routes/api"
	"github.com/zekihan/external-secrets-sops/routes/errorRoutes"
	"github.com/zekihan/external-secrets-sops/routes/middleware"
	"github.com/zekihan/external-secrets-sops/utils"
	"net/http"
	"os"
)

var stdChain middleware.Chain
var cfg *config.Config

func init() {
	envFile := ".env"
	if f, err := os.Stat(envFile); !(os.IsNotExist(err) || f.IsDir()) {
		err = godotenv.Load()
		if err != nil {
			panic("Error loading .env file")
		}
	}

	// init config
	cfg = config.GetConfig()

	// init logger
	utils.InitLogger(cfg)

	// init middleware
	stdChain = middleware.New(middleware.RecoverHandler, middleware.Logging)
}

func main() {
	log.Info("Starting external secrets sops server")
	log.Info(utils.GetStartupInfo())

	go func() {
		git.StartPullLoop()
	}()

	// API routes
	http.HandleFunc("/api/ping", api.GetPing)
	http.HandleFunc("/api/health", api.GetHealth)
	http.HandleFunc("/api/ready", api.GetReady)

	// git routes
	http.HandleFunc("/git/lastCommit", api.GetCommit)
	http.HandleFunc("/git/forceSync", api.ForceSync)

	// Secret routes
	http.HandleFunc("/secret/{project}/{config}/{environment}", api.GetAllSecret)
	http.HandleFunc("/secret/{project}/{config}/{environment}/{key}", api.GetSecret)

	// Error routes
	http.HandleFunc("/404", errorRoutes.GetNotFound)
	http.HandleFunc("/5xx", errorRoutes.Get5xx)

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
