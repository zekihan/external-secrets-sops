package secret

import (
	sops "github.com/getsops/sops/v3/decrypt"
	"github.com/zekihan/external-secrets-sops/config"
	"gopkg.in/yaml.v3"
	"path"
)

func GetSecret(project string, config string, environment string, key string) (string, error) {
	values, err := GetAllSecrets(project, config, environment)
	if err != nil {
		return "", err
	}
	return values[key], nil
}

func GetAllSecrets(project string, config string, environment string) (map[string]string, error) {
	secretPath := getSecretPath(project, config, environment)
	decrypted, err := sops.File(secretPath, path.Ext(secretPath))
	if err != nil {
		return nil, err
	}
	values := make(map[string]string)
	err = yaml.Unmarshal(decrypted, &values)
	if err != nil {
		return nil, err
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
