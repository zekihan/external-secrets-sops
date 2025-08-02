package api

import (
	"net/http"

	"github.com/zekihan/go-commons/web"

	"github.com/zekihan/external-secrets-sops/git"
)

func GetCommit(w http.ResponseWriter, _ *http.Request) {
	commit, err := git.GetLastCommit()
	if web.CheckError(w, err, http.StatusInternalServerError, "Failed to get last commit") {
		return
	}

	web.WriteString(w, commit.String()+"\n")
}
