package api

import (
	"github.com/zekihan/external-secrets-sops/git"
	"github.com/zekihan/external-secrets-sops/routes"
	"net/http"
)

func GetCommit(w http.ResponseWriter, _ *http.Request) {
	commit, err := git.GetLastCommit()
	if routes.CheckError(w, err, http.StatusInternalServerError, "Failed to get last commit") {
		return
	}
	routes.WriteString(w, commit.String()+"\n")
}
