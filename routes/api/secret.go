package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/zekihan/go-commons/web"

	"github.com/zekihan/external-secrets-sops/secret"
)

func GetSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	config := r.PathValue("config")
	environment := r.PathValue("environment")
	key := r.PathValue("key")
	slog.DebugContext(r.Context(),
		"Request received for GetSecret",
		slog.String("project", project),
		slog.String("config", config),
		slog.String("environment", environment),
		slog.String("key", key),
	)

	getSecret, err := secret.GetSecret(project, config, environment, key)
	if errors.Is(err, secret.ErrInvalidPath) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)

		return
	}

	if errors.Is(err, secret.ErrKeyNotFound) {
		http.NotFound(w, r)

		return
	}

	if web.CheckError(r.Context(), w, err, http.StatusInternalServerError, "Failed to get secret") {
		return
	}

	response := new(Response)
	response.Value = getSecret

	bytes, err := json.Marshal(response)
	if web.CheckError(
		r.Context(),
		w,
		err,
		http.StatusInternalServerError,
		"Failed to marshal response",
	) {
		return
	}

	web.Write(r.Context(), w, bytes)
}

func GetAllSecret(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	config := r.PathValue("config")
	environment := r.PathValue("environment")
	slog.DebugContext(r.Context(),
		"Request received for GetAllSecret",
		slog.String("project", project),
		slog.String("config", config),
		slog.String("environment", environment),
	)

	secrets, err := secret.GetAllSecretsComputed(project, config, environment)
	if errors.Is(err, secret.ErrInvalidPath) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)

		return
	}

	if web.CheckError(r.Context(), w, err, http.StatusInternalServerError, "Failed to get secret") {
		return
	}

	response := new(Response)
	response.Values = secrets

	bytes, err := json.Marshal(response)
	if web.CheckError(
		r.Context(),
		w,
		err,
		http.StatusInternalServerError,
		"Failed to marshal response",
	) {
		return
	}

	web.Write(r.Context(), w, bytes)
}

type Response struct {
	Values map[string]string `json:"values,omitempty"`
	Value  string            `json:"value,omitempty"`
}
