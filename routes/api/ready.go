package api

import (
	"net/http"

	"github.com/zekihan/go-commons/web"

	"github.com/zekihan/external-secrets-sops/git"
)

func GetReady(w http.ResponseWriter, _ *http.Request) {
	if git.IsRepoReady() {
		web.WriteString(w, "OK\n")

		return
	}

	w.WriteHeader(http.StatusServiceUnavailable)
	web.WriteString(w, "Service is not ready\n")
}
