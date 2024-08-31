package api

import (
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/external-secrets-sops/routes"
	"net/http"
)

func ForceSync(w http.ResponseWriter, r *http.Request) {
	err := git.Pull()
	if routes.CheckError(w, err, http.StatusInternalServerError, "Failed to pull") {
		return
	}
	routes.WriteString(w, "Synced\n")
}
