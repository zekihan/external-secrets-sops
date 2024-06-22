package routes

import (
	"github.com/zekihan/external-secrets-sops/git"
	"io"
	"net/http"
)

func GetReady(w http.ResponseWriter, _ *http.Request) {
	if !git.IsRepoReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, err := io.WriteString(w, "Service is not ready\n")
		if err != nil {
			return
		}
		return
	}

	_, err := io.WriteString(w, "OK\n")
	if err != nil {
		return
	}
}
