package secret

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	sops "github.com/getsops/sops/v3/decrypt"

	"github.com/zekihan/external-secrets-sops/config"
	"github.com/zekihan/external-secrets-sops/internal/reposync"
)

var (
	ErrKeyNotFound = errors.New("key not found")
	ErrInvalidPath = errors.New("invalid path component")
)

func GetSecret(project, configName, environment, key string) (string, error) {
	values, err := GetAllSecretsComputed(project, configName, environment)
	if err != nil {
		return "", err
	}

	value, ok := values[key]
	if !ok {
		return "", ErrKeyNotFound
	}

	return value, nil
}

func GetAllSecretsComputed(project, configName, environment string) (map[string]string, error) {
	values := make(map[string]string)

	if environment != "base" {
		baseValues, err := GetAllSecrets(project, configName, "base")
		if err != nil {
			return nil, err
		}

		maps.Copy(values, baseValues)
	}

	confValues, err := GetAllSecrets(project, configName, environment)
	if err != nil {
		return nil, err
	}

	maps.Copy(values, confValues)

	return values, nil
}

func GetAllSecrets(project, configName, environment string) (map[string]string, error) {
	for _, comp := range []string{project, configName, environment} {
		if !isValidPathComponent(comp) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPath, comp)
		}
	}

	values := make(map[string]string)

	secretPath := getSecretPath(project, configName, environment)

	root := getSecretsPath()
	if !strings.HasPrefix(
		filepath.Clean(secretPath),
		filepath.Clean(root)+string(os.PathSeparator),
	) {
		return nil, fmt.Errorf("%w: path escapes secrets root", ErrInvalidPath)
	}

	reposync.Mu.RLock()
	defer reposync.Mu.RUnlock()

	_, err := os.Stat(secretPath)
	if err != nil && os.IsNotExist(err) {
		return values, nil
	} else if err != nil {
		return values, fmt.Errorf("failed to access secret file %s: %w", secretPath, err)
	}

	decrypted, err := sops.File(secretPath, path.Ext(secretPath))
	if err != nil {
		return values, fmt.Errorf("failed to decrypt secret file %s: %w", secretPath, err)
	}

	err = yaml.Unmarshal(decrypted, &values)
	if err != nil {
		return values, fmt.Errorf("failed to parse secret file %s: %w", secretPath, err)
	}

	return values, nil
}

func isValidPathComponent(s string) bool {
	return s != "" && s != "." && s != ".." &&
		!strings.Contains(s, "/") && !strings.Contains(s, "\\") &&
		!strings.Contains(s, string(os.PathSeparator))
}

func getSecretsPath() string {
	repoPath := config.GetConfig().GitConfig.RepoPath
	secretsPath := config.GetConfig().GitConfig.SecretsPath

	return path.Join(repoPath, secretsPath)
}

func getSecretPath(project, configName, environment string) string {
	secretPath := getSecretsPath()

	return path.Join(secretPath, project, configName, environment, "values.yaml")
}
