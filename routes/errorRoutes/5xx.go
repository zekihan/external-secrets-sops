package errorRoutes

import (
	"net/http"
)

func Get5xx(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
