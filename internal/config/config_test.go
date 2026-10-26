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
		{"valid", "", false}, {"unknown", "typo: true\n", true}, {"legacy", "web:\n  host: localhost\n", true}, {"multiple", "---\n{}\n", true}, {"duplicate", "scope: {}\n", true}, {"duration", "refresh:\n  timeout: -1s\n", true}, {"same scope", "scope:\n  projects: [x, x]\n  environments: [dev]\n", true},
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
