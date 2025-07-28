package api

import (
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/go-commons/web"
	"net/http"
)

func GetReady(w http.ResponseWriter, _ *http.Request) {
	if git.IsRepoReady() {
		web.WriteString(w, "OK\n")
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	web.WriteString(w, "Service is not ready\n")
}
