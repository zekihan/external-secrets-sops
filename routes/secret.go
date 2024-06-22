package routes

import (
	"encoding/json"
	"github.com/zekihan/external-secrets-sops/secret"
	"net/http"
)

func GetSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	config := r.PathValue("config")
	environment := r.PathValue("environment")
	key := r.PathValue("key")
	getSecret, err := secret.GetSecret(project, config, environment, key)
	if err != nil {
		return
	}
	response := Response{Value: getSecret}
	bytes, err := json.Marshal(response)
	if err != nil {
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
	secrets, err := secret.GetAllSecrets(project, config, environment)
	if err != nil {
		return
	}
	response := Response{Values: secrets}
	bytes, err := json.Marshal(response)
	if err != nil {
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
