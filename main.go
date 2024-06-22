package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/external-secrets-sops/config"
	"github.com/zekihan/external-secrets-sops/routes"
	"net/http"
	"os"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetLevel(config.GetConfig().LogConfig.Level)

	http.HandleFunc("/ping", routes.GetPing)

	port := config.GetConfig().WebConfig.Port
	log.Infof("Starting server on port %d", port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	if err != nil {
		log.Fatal(err)
	}
}
