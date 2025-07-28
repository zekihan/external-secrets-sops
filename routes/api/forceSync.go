package api

import (
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/go-commons/web"
	"net/http"
)

func ForceSync(w http.ResponseWriter, r *http.Request) {
	err := git.Pull()
	if web.CheckError(w, err, http.StatusInternalServerError, "Failed to pull") {
		return
	}
	web.WriteString(w, "Synced\n")
}
