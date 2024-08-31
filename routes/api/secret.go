package api

import (
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/external-secrets-sops/routes"
	"github.com/zekihan/external-secrets-sops/secret"
	"net/http"
)

func GetSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	config := r.PathValue("config")
	environment := r.PathValue("environment")
	key := r.PathValue("key")
	log.Debug("Endpoint: getSecret ", "project: ", project, " config: ", config, " environment: ", environment, " key: ", key)
	getSecret, err := secret.GetSecret(project, config, environment, key)
	if routes.CheckError(w, err, http.StatusInternalServerError, "Failed to get secret") {
		return
	}
	response := Response{Value: getSecret}
	bytes, err := json.Marshal(response)
	if routes.CheckError(w, err, http.StatusInternalServerError, "Failed to marshal response") {
		return
	}
	routes.Write(w, bytes)
}

func GetAllSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	config := r.PathValue("config")
	environment := r.PathValue("environment")
	log.Debug("Endpoint: getAllSecret ", "project: ", project, " config: ", config, " environment: ", environment)
	secrets, err := secret.GetAllSecretsComputed(project, config, environment)
	if routes.CheckError(w, err, http.StatusInternalServerError, "Failed to get secret") {
		return
	}
	response := Response{Values: secrets}
	bytes, err := json.Marshal(response)
	if routes.CheckError(w, err, http.StatusInternalServerError, "Failed to marshal response") {
		return
	}
	routes.Write(w, bytes)
}

type Response struct {
	Value  string            `json:"value,omitempty"`
	Values map[string]string `json:"values,omitempty"`
}
