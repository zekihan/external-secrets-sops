package errorRoutes

import (
	"net/http"
)

func GetNotFound(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
}
