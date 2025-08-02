package secret

import (
	"fmt"
	"os"
	"path"

	"gopkg.in/yaml.v3"

	sops "github.com/getsops/sops/v3/decrypt"

	"github.com/zekihan/external-secrets-sops/config"
)

func GetSecret(project, configName, environment, key string) (string, error) {
	values, err := GetAllSecretsComputed(project, configName, environment)
	if err != nil {
		return "", err
	}

	return values[key], nil
}

func GetAllSecretsComputed(project, configName, environment string) (map[string]string, error) {
	values := make(map[string]string)

	if environment != "base" {
		baseValues, err := GetAllSecrets(project, configName, "base")
		if err != nil {
			return nil, err
		}

		for key, value := range baseValues {
			values[key] = value
		}
	}

	confValues, err := GetAllSecrets(project, configName, environment)
	if err != nil {
		return nil, err
	}

	for key, value := range confValues {
		values[key] = value
	}

	return values, nil
}

func GetAllSecrets(project, configName, environment string) (map[string]string, error) {
	values := make(map[string]string)

	secretPath := getSecretPath(project, configName, environment)

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

func getSecretsPath() string {
	repoPath := config.GetConfig().GitConfig.RepoPath
	secretsPath := config.GetConfig().GitConfig.SecretsPath

	return path.Join(repoPath, secretsPath)
}

func getSecretPath(project, configName, environment string) string {
	secretPath := getSecretsPath()

	return path.Join(secretPath, project, configName, environment, "values.yaml")
}
