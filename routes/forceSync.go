package routes

import (
	"github.com/zekihan/external-secrets-sops/git"
	"io"
	"net/http"
)

func ForceSync(w http.ResponseWriter, r *http.Request) {
	err := git.Pull()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := io.WriteString(w, err.Error())
		if err != nil {
			return
		}
		return
	}

	_, err = io.WriteString(w, "Synced\n")
	if err != nil {
		return
	}
}
