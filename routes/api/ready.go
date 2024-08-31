package api

import (
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/external-secrets-sops/routes"
	"net/http"
)

func GetReady(w http.ResponseWriter, _ *http.Request) {
	if git.IsRepoReady() {
		routes.WriteString(w, "OK\n")
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	routes.WriteString(w, "Service is not ready\n")
}
