package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	base := "git:\n  url: ./repo\nscope:\n  projects: [project]\n  environments: [dev]\n"
	for _, tt := range []struct {
		name, suffix string
		wantErr      bool
	}{
		{"valid", "", false},
		{"non-loopback HTTP", "server:\n  address: 0.0.0.0:8080\n", true},
		{"incomplete TLS", "server:\n  tlsCertFile: cert.pem\n", true},
		{"network TLS", "server:\n  address: 0.0.0.0:8080\n  tlsCertFile: cert.pem\n  tlsKeyFile: key.pem\n", false}, {"unknown", "typo: true\n", true}, {"legacy", "web:\n  host: localhost\n", true}, {"multiple", "---\n{}\n", true}, {"duplicate", "scope: {}\n", true}, {"duration", "refresh:\n  timeout: -1s\n", true}, {"same scope", "scope:\n  projects: [x, x]\n  environments: [dev]\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(p, []byte(base+tt.suffix), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := Load(p, func(name string) string { return name })
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v", err)
			}
		})
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(p, []byte(base), 0o600)
	for _, value := range []string{"", "same"} {
		if _, _, err := Load(p, func(string) string { return value }); err == nil {
			t.Fatal("accepted missing/equal credentials")
		}
	}
}
func TestValidation(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b", "a\\b", "a\x00b"} {
		if Component(bad) {
			t.Fatalf("accepted %q", bad)
		}
	}
	c := Defaults()
	c.Git.URL = "./repo"
	c.Scope = Scope{Projects: []string{"p"}, Environments: []string{"dev"}}
	original := c.Fingerprint()
	c.Scope.Environments = []string{"prod"}
	if c.Fingerprint() == original {
		t.Fatal("scope missing from fingerprint")
	}
	for _, u := range []string{"http://example.com/repo", "https://secret@example.com/repo", "https://example.com/repo?token=secret", "git@example.com:repo"} {
		c.Git.URL = u
		if err := c.Validate(); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("URL validation failed")
		}
	}
}

func TestServerTransport(t *testing.T) {
	for _, tt := range []struct {
		address  string
		loopback bool
	}{
		{"127.0.0.1:0", true}, {"127.2.3.4:8080", true}, {"[::1]:8080", true},
		{"[::ffff:127.0.0.1]:8080", true}, {":8080", false}, {"0.0.0.0:8080", false},
		{"[::]:8080", false}, {"192.0.2.1:8080", false}, {"[2001:db8::1]:8080", false},
		{"localhost:8080", false}, {"example.invalid:8080", false}, {"[fe80::1%lo0]:8080", false},
	} {
		t.Run(tt.address, func(t *testing.T) {
			cfg := Defaults()
			cfg.Git.URL = "./repo"
			cfg.Scope = Scope{Projects: []string{"p"}, Environments: []string{"dev"}}
			cfg.Server.Address = tt.address
			if err := cfg.Validate(); (err == nil) != tt.loopback {
				t.Fatalf("cleartext Validate() = %v, loopback = %v", err, tt.loopback)
			}
			cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile = "cert.pem", "key.pem"
			if err := cfg.Validate(); err != nil {
				t.Fatalf("TLS Validate() = %v", err)
			}
		})
	}
	for _, server := range []Server{
		{Address: "127.0.0.1:0", TLSCertFile: "cert.pem"},
		{Address: "127.0.0.1:0", TLSKeyFile: "key.pem"},
		{Address: "127.0.0.1:65536"}, {Address: "127.0.0.1:http"}, {Address: "::1"},
	} {
		if err := server.Validate(); err == nil {
			t.Fatalf("accepted invalid server %+v", server)
		}
	}
}

func TestTemplateLimitConfiguration(t *testing.T) {
	for _, tt := range []struct {
		field  string
		values []int64
	}{
		{"templateDepth", []int64{0, -1, 1, 32, 128, 129}},
		{"templateValueBytes", []int64{0, -1, 1, 1 << 20, 16 << 20, (16 << 20) + 1}},
		{"templateEvaluations", []int64{0, -1, 1, 100000, 1000000, 1000001}},
	} {
		for i, value := range tt.values {
			path := filepath.Join(t.TempDir(), "config.yaml")
			text := fmt.Sprintf(
				"git:\n  url: ./repo\nscope:\n  projects: [p]\n  environments: [dev]\nlimits:\n  %s: %d\n",
				tt.field,
				value,
			)
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err := Load(path, func(name string) string { return name })
			valid := i >= 2 && i <= 4
			if (err == nil) != valid {
				t.Fatalf("%s=%d: %v", tt.field, value, err)
			}
		}
	}
}

func TestActiveRootDefaultAndPublishedConfigs(t *testing.T) {
	if got := Defaults().Git.Root; got != "encrypted/runtime" {
		t.Fatalf("default root = %q", got)
	}
	for _, tt := range []struct{ file, root string }{
		{"config.yaml", "encrypted/runtime"},
		{"docs/compose-config.yaml", "encrypted/runtime"},
	} {
		t.Run(tt.file, func(t *testing.T) {
			cfg, _, err := Load(filepath.Join("../..", tt.file), func(name string) string {
				return "fixture-" + name
			})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Git.Root != tt.root {
				t.Fatalf("root = %q, want %q", cfg.Git.Root, tt.root)
			}
		})
	}
}

func TestLoadConfigurationSizeLimit(t *testing.T) {
	const limit = 1 << 20
	base := "git:\n  url: ./repo\nscope:\n  projects: [project]\n  environments: [dev]\n"
	padded := base + "#" + strings.Repeat("x", limit-len(base)-2) + "\n"
	for _, tt := range []struct {
		name, text string
		wantErr    bool
	}{
		{"below limit", padded[:limit-1], false},
		{"at limit", padded, false},
		{"oversized comment", padded + "#", true},
		{"hidden unknown field", padded + "typo: true\n", true},
		{"hidden second document", padded + "---\n{}\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(filename, []byte(tt.text), 0o600); err != nil {
				t.Fatal(err)
			}
			lookups := 0
			_, creds, err := Load(filename, func(name string) string {
				lookups++
				return name
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, want error = %v", err, tt.wantErr)
			}
			if tt.wantErr && (lookups != 0 || creds != (Credentials{})) {
				t.Fatal("invalid configuration consulted or returned credentials")
			}
		})
	}
}

func TestLoadRejectsUnusableAPITokens(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yaml")
	text := "git:\n  url: ./repo\nscope:\n  projects: [project]\n  environments: [dev]\n"
	if err := os.WriteFile(filename, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"ESS_READER_TOKEN", "ESS_ADMIN_TOKEN"} {
		for ch := byte(0); ch <= 127; ch++ {
			if ch > 32 && ch != 127 {
				continue
			}
			t.Run(fmt.Sprintf("%s/%02x", role, ch), func(t *testing.T) {
				token := "PRIVATE_CANARY" + string(ch) + "suffix"
				_, creds, err := Load(filename, func(name string) string {
					if name == role {
						return token
					}
					return name
				})
				if err == nil {
					t.Fatal("accepted API token that cannot be used in a Bearer header")
				}
				if creds != (Credentials{}) || strings.Contains(err.Error(), "PRIVATE_CANARY") {
					t.Fatal("invalid API token returned or exposed")
				}
			})
		}
	}
}
