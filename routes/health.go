package routes

import (
	"io"
	"net/http"
)

func GetHealth(w http.ResponseWriter, _ *http.Request) {
	_, err := io.WriteString(w, "OK\n")
	if err != nil {
		return
	}
}
