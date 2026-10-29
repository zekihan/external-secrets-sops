package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  Server  `yaml:"server"`
	Git     Git     `yaml:"git"`
	Scope   Scope   `yaml:"scope"`
	Refresh Refresh `yaml:"refresh"`
	Redis   Redis   `yaml:"redis"`
	Auth    Auth    `yaml:"auth"`
	Limits  Limits  `yaml:"limits"`
}
type Server struct {
	Address         string        `yaml:"address"`
	LogLevel        string        `yaml:"logLevel"`
	ShutdownTimeout time.Duration `yaml:"shutdownTimeout"`
	TLSCertFile     string        `yaml:"tlsCertFile"`
	TLSKeyFile      string        `yaml:"tlsKeyFile"`
}
type Git struct {
	URL      string `yaml:"url"`
	Branch   string `yaml:"branch"`
	Root     string `yaml:"root"`
	CacheDir string `yaml:"cacheDir"`
	TokenEnv string `yaml:"tokenEnv"`
}
type Scope struct {
	Projects     []string `yaml:"projects"`
	Environments []string `yaml:"environments"`
}
type Refresh struct {
	Interval     time.Duration `yaml:"interval"`
	Timeout      time.Duration `yaml:"timeout"`
	RetryMin     time.Duration `yaml:"retryMin"`
	RetryMax     time.Duration `yaml:"retryMax"`
	MaxStaleness time.Duration `yaml:"maxStaleness"`
}
type Redis struct {
	Enabled     bool     `yaml:"enabled"`
	Addresses   []string `yaml:"addresses"`
	Username    string   `yaml:"username"`
	PasswordEnv string   `yaml:"passwordEnv"`
	MasterName  string   `yaml:"masterName"`
	DB          int      `yaml:"db"`
	Channel     string   `yaml:"channel"`
}
type Auth struct {
	ReaderTokenEnv string `yaml:"readerTokenEnv"`
	AdminTokenEnv  string `yaml:"adminTokenEnv"`
}
type Limits struct {
	DecryptWorkers      int   `yaml:"decryptWorkers"`
	DocumentBytes       int64 `yaml:"documentBytes"`
	SnapshotBytes       int64 `yaml:"snapshotBytes"`
	TemplateDepth       int   `yaml:"templateDepth"`
	TemplateEvaluations int   `yaml:"templateEvaluations"`
	TemplateValueBytes  int64 `yaml:"templateValueBytes"`
}

type Credentials struct {
	Reader string
	Admin  string
	Git    string
	Redis  string
}

func Defaults() Config {
	return Config{
		Server: Server{
			Address:         "127.0.0.1:8080",
			LogLevel:        "info",
			ShutdownTimeout: 30 * time.Second,
		},
		Git: Git{
			Branch:   "main",
			Root:     "encrypted/runtime",
			CacheDir: "./tmp/cache",
			TokenEnv: "ESS_GIT_TOKEN",
		},
		Refresh: Refresh{
			Interval: 5 * time.Minute,
			Timeout:  2 * time.Minute,
			RetryMin: 5 * time.Second,
			RetryMax: 5 * time.Minute,
		},
		Redis: Redis{
			Addresses:   []string{"localhost:6379"},
			PasswordEnv: "ESS_REDIS_PASSWORD",
			Channel:     "ess:github-events",
		},
		Auth: Auth{ReaderTokenEnv: "ESS_READER_TOKEN", AdminTokenEnv: "ESS_ADMIN_TOKEN"},
		Limits: Limits{
			DecryptWorkers:      4,
			DocumentBytes:       1 << 20,
			SnapshotBytes:       32 << 20,
			TemplateDepth:       32,
			TemplateValueBytes:  1 << 20,
			TemplateEvaluations: 100000,
		},
	}
}

func Load(filename string, getenv func(string) string) (Config, Credentials, error) {
	cfg := Defaults()
	f, err := os.Open(filename)
	if err != nil {
		return cfg, Credentials{}, fmt.Errorf("open configuration: %w", err)
	}
	defer func() { _ = f.Close() }()
	const maxConfigurationBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxConfigurationBytes+1))
	if err != nil {
		return cfg, Credentials{}, errors.New("cannot read configuration")
	}
	if len(data) > maxConfigurationBytes {
		return cfg, Credentials{}, errors.New("configuration exceeds size limit")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err = dec.Decode(&cfg); err != nil {
		return cfg, Credentials{}, errors.New("invalid configuration YAML")
	}
	var extra any
	if err = dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return cfg, Credentials{}, errors.New("configuration must contain exactly one document")
	}
	if err = cfg.Validate(); err != nil {
		return cfg, Credentials{}, err
	}
	creds := Credentials{
		Reader: getenv(cfg.Auth.ReaderTokenEnv),
		Admin:  getenv(cfg.Auth.AdminTokenEnv),
	}
	if cfg.Git.TokenEnv != "" {
		creds.Git = getenv(cfg.Git.TokenEnv)
	}
	if cfg.Redis.Enabled && cfg.Redis.PasswordEnv != "" {
		creds.Redis = getenv(cfg.Redis.PasswordEnv)
	}
	if creds.Reader == "" || creds.Admin == "" || creds.Reader == creds.Admin {
		return cfg, Credentials{}, errors.New(
			"reader and admin tokens must be present and distinct",
		)
	}
	if strings.ContainsAny(creds.Reader+creds.Admin, " \t\r\n") {
		return cfg, Credentials{}, errors.New("API tokens must not contain whitespace")
	}
	if cfg.Redis.Enabled && cfg.Redis.PasswordEnv != "" && creds.Redis == "" {
		return cfg, Credentials{}, errors.New(
			"configured Redis password environment variable is empty",
		)
	}
	return cfg, creds, nil
}

func Component(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if r < ' ' || r == 127 || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func (s Server) Validate() error {
	host, port, err := net.SplitHostPort(s.Address)
	if err != nil {
		return errors.New("server.address must be host:port")
	}
	if n, e := strconv.Atoi(port); e != nil || n < 0 || n > 65535 {
		return errors.New("invalid server port")
	}
	if (s.TLSCertFile == "") != (s.TLSKeyFile == "") {
		return errors.New("server.tlsCertFile and server.tlsKeyFile must be configured together")
	}
	if s.TLSCertFile == "" && !net.ParseIP(host).IsLoopback() {
		return errors.New(
			"non-loopback server.address requires server.tlsCertFile and server.tlsKeyFile; HTTP requires a literal loopback IP",
		)
	}
	return nil
}

func (c Config) Validate() error {
	if err := c.Server.Validate(); err != nil {
		return err
	}
	if err := plumbing.NewBranchReferenceName(c.Git.Branch).Validate(); err != nil {
		return errors.New("invalid Git branch")
	}
	switch c.Server.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("invalid server.logLevel")
	}
	if c.Server.ShutdownTimeout <= 0 || c.Refresh.Interval <= 0 || c.Refresh.Timeout <= 0 ||
		c.Refresh.RetryMin <= 0 ||
		c.Refresh.RetryMax < c.Refresh.RetryMin ||
		c.Refresh.MaxStaleness < 0 {
		return errors.New("invalid refresh or shutdown durations")
	}
	if c.Git.URL == "" || c.Git.CacheDir == "" || c.Git.Branch == "" {
		return errors.New("git.url, git.branch and git.cacheDir are required")
	}
	if strings.Contains(c.Git.URL, "://") {
		u, err := url.Parse(c.Git.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" ||
			u.Fragment != "" {
			return errors.New("git.url must be HTTPS without embedded credentials, or a local path")
		}
	} else if !filepath.IsAbs(c.Git.URL) && !strings.HasPrefix(c.Git.URL, "./") && !strings.HasPrefix(c.Git.URL, "../") {
		return errors.New("local git.url must be absolute or start with ./ or ../")
	}
	if c.Git.Root == "" || path.IsAbs(c.Git.Root) || path.Clean(c.Git.Root) != c.Git.Root ||
		strings.Contains(c.Git.Root, "\\") ||
		c.Git.Root == ".." ||
		strings.HasPrefix(c.Git.Root, "../") {
		return errors.New("git.root must be a clean repository-relative path")
	}
	if len(c.Scope.Projects) == 0 || len(c.Scope.Environments) == 0 {
		return errors.New("scope.projects and scope.environments are required")
	}
	for _, list := range [][]string{c.Scope.Projects, c.Scope.Environments} {
		seen := map[string]bool{}
		for _, v := range list {
			if !Component(v) || seen[v] {
				return errors.New("scope entries must be unique path components")
			}
			seen[v] = true
		}
	}
	if c.Auth.ReaderTokenEnv == "" || c.Auth.AdminTokenEnv == "" {
		return errors.New("authorization environment references are required")
	}
	if c.Limits.DecryptWorkers < 1 || c.Limits.DecryptWorkers > 64 || c.Limits.DocumentBytes < 1 ||
		c.Limits.SnapshotBytes < c.Limits.DocumentBytes {
		return errors.New("invalid document, snapshot or worker limits")
	}
	if c.Limits.TemplateDepth < 1 || c.Limits.TemplateDepth > 128 ||
		c.Limits.TemplateValueBytes < 1 ||
		c.Limits.TemplateValueBytes > 16<<20 ||
		c.Limits.TemplateEvaluations < 1 ||
		c.Limits.TemplateEvaluations > 1000000 {
		return errors.New("invalid template limits")
	}
	if c.Redis.Enabled && (len(c.Redis.Addresses) == 0 || c.Redis.Channel == "" || c.Redis.DB < 0) {
		return errors.New("invalid Redis configuration")
	}
	for _, address := range c.Redis.Addresses {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("Redis addresses must be host:port")
		}
	}
	return nil
}

func (c Config) Fingerprint() string {
	b, _ := json.Marshal(struct {
		Version int
		Git     Git
		Scope   Scope
		Limits  Limits
	}{1, c.Git, c.Scope, c.Limits})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
