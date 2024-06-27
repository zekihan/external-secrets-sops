package config

import (
	"context"
	"github.com/sethvargo/go-envconfig"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"os"
	"time"
)

var config *Config

type Config struct {
	WebConfig WebConfig `yaml:"web" env:", prefix=ESS__WEB_"`
	LogConfig LogConfig `yaml:"log" env:", prefix=ESS__LOG_"`
	GitConfig GitConfig `yaml:"git" env:", prefix=ESS__GIT_"`
}

type WebConfig struct {
	Port int `yaml:"port" env:"PORT"`
}

type LogConfig struct {
	Level         logrus.Level `yaml:"-"`
	LevelStr      string       `yaml:"level" env:"LEVEL"`
	AddMethodName bool         `yaml:"add_method_name" env:"ADD_METHOD_NAME"`
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
		if err := envconfig.Process(ctx, cfg); err != nil {
			log.Fatal(err)
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
	file, err := os.Open("config.yaml")
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

func GetDefaultConfig() *Config {
	return &Config{
		WebConfig: WebConfig{
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
