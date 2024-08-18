package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/external-secrets-sops/config"
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/external-secrets-sops/routes"
	"github.com/zekihan/external-secrets-sops/utils"
	"net/http"
	"os"
)

func init() {
	config.GetConfig()
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	log.SetOutput(os.Stdout)
	log.SetLevel(config.GetConfig().LogConfig.Level)
	log.SetReportCaller(config.GetConfig().LogConfig.AddMethodName)
}

func main() {
	log.Infof("Starting external secrets sops %s in %s", utils.GetVersion(), utils.GetEnvironment())

	go func() {
		git.StartPullLoop()
	}()

	http.HandleFunc("/ping", routes.GetPing)
	http.HandleFunc("/health", routes.GetHealth)
	http.HandleFunc("/ready", routes.GetReady)
	http.HandleFunc("/git/lastCommit", routes.GetCommit)
	http.HandleFunc("/git/forceSync", routes.ForceSync)
	http.HandleFunc("/secret/{project}/{config}/{environment}", routes.GetAllSecret)
	http.HandleFunc("/secret/{project}/{config}/{environment}/{key}", routes.GetSecret)

	host := config.GetConfig().WebConfig.Host
	port := config.GetConfig().WebConfig.Port
	log.Infof("Starting web server on %s:%d", host, port)
	err := http.ListenAndServe(fmt.Sprintf("%s:%d", host, port), nil)
	if err != nil {
		log.Fatal(err)
	}

	git.StopPullLoop()
}
