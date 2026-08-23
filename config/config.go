package config

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/sethvargo/go-envconfig"
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
	Host   string `yaml:"host"   env:"HOST"`
	APIKey string `yaml:"apiKey" env:"API_KEY"`
	Port   int    `yaml:"port"   env:"PORT"`
}

type LogConfig struct {
	LevelStr  string     `yaml:"level"     env:"LEVEL"`
	Path      string     `yaml:"path"      env:"PATH"`
	Level     slog.Level `yaml:"-"`
	AddSource bool       `yaml:"addSource" env:"ADD_SOURCE"`
}

type GitConfig struct {
	RepoPath     string        `yaml:"repoPath"     env:"REPO_PATH"`
	RepoURL      string        `yaml:"repoUrl"      env:"REPO_URL"`
	AccessToken  string        `yaml:"accessToken"  env:"ACCESS_TOKEN"`
	Branch       string        `yaml:"branch"       env:"BRANCH"`
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
	return GetConfigWithContext(context.Background())
}

func GetConfigWithContext(ctx context.Context) *Config {
	if config == nil {
		cfg := GetConfigFromFile(ctx)

		envConfig := new(envconfig.Config)
		envConfig.Target = cfg
		envConfig.Lookuper = envconfig.PrefixLookuper(EnvKeyPrefix, envconfig.OsLookuper())
		envConfig.DefaultOverwrite = true

		err := envconfig.ProcessWith(ctx, envConfig)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to process env config", slog.Any("error", err))
			os.Exit(1) //nolint:forbidigo // This package configures log packages.
		}

		if cfg.LogConfig.LevelStr != "" {
			level := slog.LevelInfo

			switch strings.ToLower(cfg.LogConfig.LevelStr) {
			case "debug":
				level = slog.LevelDebug
			case "info":
				level = slog.LevelInfo
			case "warn", "warning":
				level = slog.LevelWarn
			case "error":
				level = slog.LevelError
			default:
				slog.ErrorContext(
					ctx,
					"Invalid log level in config, defaulting to info",
					slog.String("level", cfg.LogConfig.LevelStr),
				)
			}

			cfg.LogConfig.Level = level
		}

		config = cfg
	}

	return config
}

func GetConfigFromFile(ctx context.Context) *Config {
	newConfig := GetDefaultConfig()

	file, err := os.Open(getConfigFilePath(ctx))
	if err != nil {
		slog.ErrorContext(
			ctx,
			"Failed to open config file",
			slog.Any("error", err),
		)
		os.Exit(1) //nolint:forbidigo // This package configures log packages.
	}
	defer func(file *os.File) {
		fileErr := file.Close()
		if fileErr != nil {
			slog.ErrorContext(ctx, "Failed to close config file", slog.Any("error", fileErr))
		}
	}(file)

	content, err := io.ReadAll(file)
	if err != nil {
		slog.ErrorContext(
			ctx,
			"Failed to read config file",
			slog.Any("error", err),
		)
		//nolint:forbidigo,gocritic // This package configures log packages.
		os.Exit(1)
	}

	err = yaml.Unmarshal(content, newConfig)
	if err != nil {
		slog.ErrorContext(
			ctx,
			"Failed to parse config file",
			slog.Any("error", err),
		)
		//nolint:forbidigo // This package configures log packages.
		os.Exit(1)
	}

	return newConfig
}

func getConfigFilePath(ctx context.Context) string {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath != "" {
		return configPath
	}

	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			slog.ErrorContext(
				ctx,
				"Failed to get current working directory",
				slog.Any("error", err),
			)
			os.Exit(1) //nolint:forbidigo // This package configures log packages.
		}

		configDir = cwd
	}

	configPath = path.Join(configDir, defaultConfigFile)

	return configPath
}

func GetDefaultConfig() *Config {
	defaultConfig := new(Config)
	defaultConfig.WebConfig = new(WebConfig)
	defaultConfig.WebConfig.Host = "127.0.0.1"
	defaultConfig.WebConfig.Port = defaultPort

	defaultConfig.LogConfig = new(LogConfig)
	defaultConfig.LogConfig.Level = slog.LevelWarn

	defaultConfig.GitConfig = new(GitConfig)
	defaultConfig.GitConfig.RepoPath = "./tmp/repo"
	defaultConfig.GitConfig.Branch = "main"
	defaultConfig.GitConfig.PullInterval = defaultPullInterval
	defaultConfig.GitConfig.SecretsPath = "./"

	defaultConfig.RedisConfig = new(RedisConfig)
	defaultConfig.RedisConfig.Addrs = make([]string, 0)
	defaultConfig.RedisConfig.Prefix = "ess"

	return defaultConfig
}
