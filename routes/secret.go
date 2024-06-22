package routes

import (
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/external-secrets-sops/secret"
	"io"
	"net/http"
)

func GetSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	config := r.PathValue("config")
	environment := r.PathValue("environment")
	key := r.PathValue("key")
	log.Debug("Endpoint: getSecret ", "project: ", project, " config: ", config, " environment: ", environment, " key: ", key)
	getSecret, err := secret.GetSecret(project, config, environment, key)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := io.WriteString(w, err.Error())
		if err != nil {
			return
		}
		return
	}
	response := Response{Value: getSecret}
	bytes, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := io.WriteString(w, err.Error())
		if err != nil {
			return
		}
		return
	}
	_, err = w.Write(bytes)
	if err != nil {
		return
	}
}

func GetAllSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	config := r.PathValue("config")
	environment := r.PathValue("environment")
	log.Debug("Endpoint: getAllSecret ", "project: ", project, " config: ", config, " environment: ", environment)
	secrets, err := secret.GetAllSecrets(project, config, environment)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := io.WriteString(w, err.Error())
		if err != nil {
			return
		}
		return
	}
	response := Response{Values: secrets}
	bytes, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := io.WriteString(w, err.Error())
		if err != nil {
			return
		}
		return
	}
	_, err = w.Write(bytes)
	if err != nil {
		return
	}
}

type Response struct {
	Value  string            `json:"value,omitempty"`
	Values map[string]string `json:"values,omitempty"`
}
