package api

import (
	"net/http"

	"github.com/zekihan/go-commons/web"

	"github.com/zekihan/external-secrets-sops/git"
)

func GetCommit(w http.ResponseWriter, r *http.Request) {
	commit, err := git.GetLastCommit(r.Context())
	if web.CheckError(
		r.Context(),
		w,
		err,
		http.StatusInternalServerError,
		"Failed to get last commit",
	) {
		return
	}

	web.WriteString(r.Context(), w, commit.String()+"\n")
}
