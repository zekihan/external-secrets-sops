package config

import (
	"context"
	"github.com/sethvargo/go-envconfig"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"os"
	"path"
	"time"
)

const EnvKeyPrefix = "ESS__"
const defaultConfigFile = "config.yaml"

var config *Config

type Config struct {
	WebConfig WebConfig `yaml:"web" env:", prefix=WEB_"`
	LogConfig LogConfig `yaml:"log" env:", prefix=LOG_"`
	GitConfig GitConfig `yaml:"git" env:", prefix=GIT_"`
}

type WebConfig struct {
	Host string `yaml:"host" env:"HOST"`
	Port int    `yaml:"port" env:"PORT"`
}

type LogConfig struct {
	Level         logrus.Level `yaml:"-"`
	LevelStr      string       `yaml:"level" env:"LEVEL"`
	AddMethodName bool         `yaml:"add_method_name" env:"ADD_METHOD_NAME"`
	Path          string       `yaml:"path" env:"PATH"`
}

type GitConfig struct {
	RepoPath     string        `yaml:"repo_path" env:"REPO_PATH"`
	RepoURL      string        `yaml:"repo_url" env:"REPO_URL"`
	AccessToken  string        `yaml:"access_token" env:"ACCESS_TOKEN"`
	PullInterval time.Duration `yaml:"pull_interval" env:"PULL_INTERVAL"`
	SecretsPath  string        `yaml:"secrets_path" env:"SECRETS_PATH"`
}

func GetConfig() *Config {
	if config == nil {
		cfg := GetConfigFromFile()
		ctx := context.Background()
		if err := envconfig.ProcessWith(ctx, &envconfig.Config{
			Target:           cfg,
			Lookuper:         envconfig.PrefixLookuper(EnvKeyPrefix, envconfig.OsLookuper()),
			DefaultOverwrite: true,
		}); err != nil {
			log.Fatalf("Failed to process env config: %v", err)
		}
		if cfg.LogConfig.LevelStr != "" {
			level, err := logrus.ParseLevel(cfg.LogConfig.LevelStr)
			if err != nil {
				log.Fatalf("Failed to parse log level: %v", err)
			}
			cfg.LogConfig.Level = level
		}
		config = cfg
	}
	return config
}

func GetConfigFromFile() *Config {
	config := GetDefaultConfig()
	file, err := os.Open(getConfigFilePath())
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}
	err = yaml.Unmarshal(content, config)
	if err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}
	return config
}

func getConfigFilePath() string {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath != "" {
		return configPath
	}
	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("%v", err)
		}
		configDir = cwd
	}
	configPath = path.Join(configDir, defaultConfigFile)
	return configPath
}

func GetDefaultConfig() *Config {
	return &Config{
		WebConfig: WebConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
		LogConfig: LogConfig{
			Level:         logrus.WarnLevel,
			AddMethodName: false,
		},
		GitConfig: GitConfig{
			RepoPath:     "./tmp/repo",
			RepoURL:      "",
			AccessToken:  "",
			PullInterval: time.Minute * 15,
			SecretsPath:  "./",
		},
	}
}
