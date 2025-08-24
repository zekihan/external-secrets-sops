package api

import (
	"net/http"

	"github.com/zekihan/go-commons/web"

	"github.com/zekihan/external-secrets-sops/pubsub"
)

func ForceSync(w http.ResponseWriter, r *http.Request) {
	message := "Manual sync triggered via API"

	githubDeliveryID := r.Header.Get("X-Github-Delivery")
	if githubDeliveryID != "" {
		message = "Sync triggered via API with GitHub Delivery ID: " + githubDeliveryID
	}

	err := pubsub.PublishGithubEvent(r.Context(), message)
	if web.CheckError(w, err, http.StatusInternalServerError, "Failed to publish github event") {
		return
	}

	web.WriteString(w, "Sync event published successfully\n")
}
