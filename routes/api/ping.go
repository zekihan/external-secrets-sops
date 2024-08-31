package api

import (
	"github.com/zekihan/external-secrets-sops/routes"
	"net/http"
)

func GetPing(w http.ResponseWriter, _ *http.Request) {
	routes.WriteString(w, "Pong\n")
}
