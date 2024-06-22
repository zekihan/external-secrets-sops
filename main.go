package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/external-secrets-sops/config"
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/external-secrets-sops/routes"
	"net/http"
	"os"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetLevel(config.GetConfig().LogConfig.Level)

	go func() {
		git.StartPullLoop()
	}()

	http.HandleFunc("/ping", routes.GetPing)
	http.HandleFunc("/health", routes.GetHealth)
	http.HandleFunc("/ready", routes.GetReady)
	http.HandleFunc("/secret/{project}/{config}/{environment}", routes.GetAllSecret)
	http.HandleFunc("/secret/{project}/{config}/{environment}/{key}", routes.GetSecret)

	port := config.GetConfig().WebConfig.Port
	log.Infof("Starting server on port %d", port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	if err != nil {
		log.Fatal(err)
	}

	git.StopPullLoop()
}
