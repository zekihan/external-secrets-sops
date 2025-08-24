//nolint:forbidigo // This package configures log packages.
package config

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"time"

	"github.com/sethvargo/go-envconfig"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

const (
	EnvKeyPrefix        = "ESS__"
	defaultConfigFile   = "config.yaml"
	defaultPort         = 8080
	defaultPullInterval = 15 * time.Minute
)

var config *Config

type Config struct {
	GitConfig   *GitConfig   `yaml:"git"   env:", prefix=GIT_"`
	LogConfig   *LogConfig   `yaml:"log"   env:", prefix=LOG_"`
	WebConfig   *WebConfig   `yaml:"web"   env:", prefix=WEB_"`
	RedisConfig *RedisConfig `yaml:"redis" env:", prefix=REDIS_"`
}

type WebConfig struct {
	Host string `yaml:"host" env:"HOST"`
	Port int    `yaml:"port" env:"PORT"`
}

type LogConfig struct {
	LevelStr      string       `yaml:"level"         env:"LEVEL"`
	Path          string       `yaml:"path"          env:"PATH"`
	Level         logrus.Level `yaml:"-"`
	AddMethodName bool         `yaml:"addMethodName" env:"ADD_METHOD_NAME"`
}

type GitConfig struct {
	RepoPath     string        `yaml:"repoPath"     env:"REPO_PATH"`
	RepoURL      string        `yaml:"repoUrl"      env:"REPO_URL"`
	AccessToken  string        `yaml:"accessToken"  env:"ACCESS_TOKEN"`
	SecretsPath  string        `yaml:"secretsPath"  env:"SECRETS_PATH"`
	PullInterval time.Duration `yaml:"pullInterval" env:"PULL_INTERVAL"`
}

type RedisConfig struct {
	Username         string   `yaml:"username"         env:"USERNAME"`
	Password         string   `yaml:"password"         env:"PASSWORD"`
	SentinelUsername string   `yaml:"sentinelUsername" env:"SENTINEL_USERNAME"`
	SentinelPassword string   `yaml:"sentinelPassword" env:"SENTINEL_PASSWORD"`
	MasterName       string   `yaml:"masterName"       env:"MASTER_NAME"`
	Prefix           string   `yaml:"prefix"           env:"PREFIX"`
	Addrs            []string `yaml:"addrs"            env:"ADDRS"`
	DB               int      `yaml:"db"               env:"DB"`
	Enabled          bool     `yaml:"enabled"          env:"ENABLED"`
}

func GetConfig() *Config {
	if config == nil {
		cfg := GetConfigFromFile()

		ctx := context.Background()

		err := envconfig.ProcessWith(ctx, &envconfig.Config{
			Target:           cfg,
			Lookuper:         envconfig.PrefixLookuper(EnvKeyPrefix, envconfig.OsLookuper()),
			DefaultOverwrite: true,
		})
		if err != nil {
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
	newConfig := GetDefaultConfig()

	file, err := os.Open(getConfigFilePath())
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	defer func(file *os.File) {
		fileErr := file.Close()
		if fileErr != nil {
			fmt.Printf("Failed to close config file: %v", fileErr)
		}
	}(file)

	content, err := io.ReadAll(file)
	if err != nil {
		fileErr := file.Close()
		if fileErr != nil {
			fmt.Printf("Failed to close config file: %v", fileErr)
		}

		log.Fatalf("Failed to read config file: %v", err) //nolint:gocritic
	}

	err = yaml.Unmarshal(content, newConfig)
	if err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	return newConfig
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
		WebConfig: &WebConfig{
			Host: "127.0.0.1",
			Port: defaultPort,
		},
		LogConfig: &LogConfig{
			Level:         logrus.WarnLevel,
			AddMethodName: false,
		},
		GitConfig: &GitConfig{
			RepoPath:     "./tmp/repo",
			RepoURL:      "",
			AccessToken:  "",
			PullInterval: defaultPullInterval,
			SecretsPath:  "./",
		},
		RedisConfig: &RedisConfig{
			Addrs:  make([]string, 0),
			DB:     0,
			Prefix: "ess",
		},
	}
}
