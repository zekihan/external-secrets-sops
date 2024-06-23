package routes

import (
	"github.com/zekihan/external-secrets-sops/git"
	"io"
	"net/http"
)

func GetCommit(w http.ResponseWriter, _ *http.Request) {
	commit, err := git.GetLastCommit()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := io.WriteString(w, err.Error())
		if err != nil {
			return
		}
		return
	}
	_, err = io.WriteString(w, commit.String()+"\n")
	if err != nil {
		return
	}
}
