package config

import (
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"os"
	"time"
)

var config *Config

type Config struct {
	WebConfig   WebConfig   `yaml:"web"`
	CacheConfig CacheConfig `yaml:"cache"`
	LogConfig   LogConfig   `yaml:"log"`
	UserAgent   string      `yaml:"user_agent"`
	GitConfig   GitConfig   `yaml:"git"`
}

type CacheConfig struct {
	MaxEntries int           `yaml:"max_entries"`
	Expiration time.Duration `yaml:"expiration"`
}

type WebConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type LogConfig struct {
	Level logrus.Level
}

type GitConfig struct {
	RepoPath     string        `yaml:"repo_path"`
	RepoURL      string        `yaml:"repo_url"`
	AccessToken  string        `yaml:"access_token"`
	PullInterval time.Duration `yaml:"pull_interval"`
	SecretsPath  string        `yaml:"secrets_path"`
}

func GetConfig() *Config {
	if config == nil {
		config = GetConfigFromFile()
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
			Host: "http://localhost",
			Port: 8080,
		},
		CacheConfig: CacheConfig{
			MaxEntries: 100,
			Expiration: time.Second * 10,
		},
		LogConfig: LogConfig{
			Level: logrus.WarnLevel,
		},
		UserAgent: "Golang_Spider_Bot/3.0",
		GitConfig: GitConfig{
			RepoPath:     "./tmp/repo",
			RepoURL:      "",
			AccessToken:  "",
			PullInterval: time.Minute * 15,
			SecretsPath:  "./",
		},
	}
}
