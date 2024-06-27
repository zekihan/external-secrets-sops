package secret

import (
	sops "github.com/getsops/sops/v3/decrypt"
	"github.com/zekihan/external-secrets-sops/config"
	"gopkg.in/yaml.v3"
	"os"
	"path"
)

func GetSecret(project string, config string, environment string, key string) (string, error) {
	values, err := GetAllSecretsComputed(project, config, environment)
	if err != nil {
		return "", err
	}
	return values[key], nil
}

func GetAllSecretsComputed(project string, config string, environment string) (map[string]string, error) {
	values := make(map[string]string)
	if environment != "base" {
		baseValues, err := GetAllSecrets(project, config, "base")
		if err != nil {
			return nil, err
		}
		for key, value := range baseValues {
			values[key] = value
		}
	}
	confValues, err := GetAllSecrets(project, config, environment)
	if err != nil {
		return nil, err
	}
	for key, value := range confValues {
		values[key] = value
	}
	return values, nil
}

func GetAllSecrets(project string, config string, environment string) (map[string]string, error) {
	values := make(map[string]string)

	secretPath := getSecretPath(project, config, environment)
	_, err := os.Stat(secretPath)
	if err != nil && os.IsNotExist(err) {
		return values, nil
	} else if err != nil {
		return values, err
	}

	decrypted, err := sops.File(secretPath, path.Ext(secretPath))
	if err != nil {
		return values, err
	}

	err = yaml.Unmarshal(decrypted, &values)
	if err != nil {
		return values, err
	}

	return values, nil
}

func getSecretsPath() string {
	repoPath := config.GetConfig().GitConfig.RepoPath
	secretsPath := config.GetConfig().GitConfig.SecretsPath
	return path.Join(repoPath, secretsPath)
}

func getSecretPath(project string, config string, environment string) string {
	secretPath := getSecretsPath()
	return path.Join(secretPath, project, config, environment, "values.yaml")
}
