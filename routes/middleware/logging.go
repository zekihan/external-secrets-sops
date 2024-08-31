package middleware

import (
	"github.com/felixge/httpsnoop"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/external-secrets-sops/utils"
	"net/http"
	"time"
)

func Logging(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, err := utils.GetIP(r)
		if err != nil {
			log.Error("Error getting IP: ", err)
			ip = "unknown"
		}
		m := httpsnoop.CaptureMetrics(handler, w, r)
		log.Infof("%s %s %d %s %s", r.Method, ip, m.Code, m.Duration.Round(time.Millisecond), r.URL.Path)
	})
}
