package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderedChart(t *testing.T) {
	dir := os.Getenv("ESS_CHART_RENDER_DIR")
	if dir == "" {
		t.Skip("set ESS_CHART_RENDER_DIR to validate the four rendered chart variants")
	}
	for _, cluster := range []string{"prod", "dev"} {
		wantEnvironments := []string{cluster}
		wantRemoteEnvironment := cluster
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-%t", cluster, enabled), func(t *testing.T) {
				name := fmt.Sprintf("%s-%t.yaml", cluster, enabled)
				f, err := os.Open(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				decoder := yaml.NewDecoder(f)
				foundConfig, foundDeployment, foundStore := false, false, false
				for {
					var document map[string]any
					err = decoder.Decode(&document)
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					switch document["kind"] {
					case "ConfigMap":
						data := document["data"].(map[string]any)
						raw, ok := data["config.yaml"].(string)
						if !ok {
							continue
						}
						file := filepath.Join(t.TempDir(), "config.yaml")
						if err = os.WriteFile(file, []byte(raw), 0o600); err != nil {
							t.Fatal(err)
						}
						cfg, _, e := Load(file, func(s string) string { return s + "-fixture" })
						if e != nil {
							t.Fatalf("rendered config invalid: %v", e)
						}
						if len(cfg.Scope.Projects) != 1 || cfg.Scope.Projects[0] != "example" ||
							!slices.Equal(cfg.Scope.Environments, wantEnvironments) ||
							cfg.Redis.Enabled != enabled {
							t.Fatal("wrong rendered scope/Redis mode")
						}
						foundConfig = true
					case "Deployment":
						spec := nested(document, "spec", "template", "spec")
						containers := spec["containers"].([]any)
						container := containers[0].(map[string]any)
						envs := container["env"].([]any)
						names := map[string]bool{}
						for _, env := range envs {
							names[env.(map[string]any)["name"].(string)] = true
						}
						if !names["ESS_READER_TOKEN"] || !names["ESS_ADMIN_TOKEN"] ||
							names["ESS_REDIS_PASSWORD"] != enabled {
							t.Fatal("wrong credential references")
						}
						for _, probe := range []string{"startupProbe", "livenessProbe", "readinessProbe"} {
							want := "/livez"
							if probe == "readinessProbe" {
								want = "/readyz"
							}
							if nested(container, probe, "httpGet")["path"] != want {
								t.Fatal("wrong probe")
							}
						}
						if nested(container, "resources", "limits")["memory"] != "256Mi" {
							t.Fatal("wrong memory limit")
						}
						if container["args"] == nil {
							t.Fatal("missing explicit config arguments")
						}
						foundDeployment = true
					case "ClusterSecretStore":
						provider := nested(document, "spec", "provider")
						webhook, ok := provider["webhook"].(map[string]any)
						if !ok {
							continue
						}
						secretURL := webhook["url"].(string)
						if !strings.Contains(secretURL, "/v1/secrets/example/") ||
							!strings.HasSuffix(secretURL, "/"+wantRemoteEnvironment) ||
							nested(webhook, "result")["jsonPath"] != "$.data" ||
							!strings.HasPrefix(
								nested(webhook, "headers")["Authorization"].(string),
								"Bearer ",
							) {
							t.Fatal("wrong ESO contract")
						}
						foundStore = true
					}
				}
				if !foundConfig || !foundDeployment || !foundStore {
					t.Fatal("missing required resources")
				}
			})
		}
	}
}
func nested(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		m = m[key].(map[string]any)
	}
	return m
}
