package snapshot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/decrypt"
	"github.com/zekihan/external-secrets-sops/internal/source"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func TestEncryptedTemplates(t *testing.T) {
	cfg := testutil.Config(t)
	cfg.Scope.Projects = append(cfg.Scope.Projects, "shared")
	cfg.Scope.Environments = []string{"dev", "prod"}
	docs := []source.Document{
		doc("base", `USER: base
PASSWORD: {$ref: shared/db/PASSWORD}
URL: {$template: 'postgres://${USER | urluserinfo}:${PASSWORD | urluserinfo}@db/app'}
LITERAL: '${MISSING}'
COPY: {$ref: LITERAL}
ESCAPED: {$template: '$${MISSING}'}
REMOTE: {$ref: shared/db/prod/PASSWORD}
'API|TOKEN': odd
ODD: {$template: '${API%7CTOKEN}'}
`),
		doc("dev", "USER: dev\n"),
		{
			Project:     "shared",
			Config:      "db",
			Environment: "dev",
			Encrypted:   []byte("PASSWORD: 'a:b@ /'\n"),
		},
		{
			Project:     "shared",
			Config:      "db",
			Environment: "prod",
			Encrypted:   []byte("PASSWORD: prod-pass\n"),
		},
	}
	for i := range docs {
		docs[i].Encrypted = testutil.Encrypt(t, string(docs[i].Encrypted))
	}
	s, err := NewBuilder(cfg, decrypt.SOPS{}).Build(t.Context(), "r", time.Now(), docs)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"URL": "postgres://dev:a%3Ab%40%20%2F@db/app", "COPY": "${MISSING}", "ESCAPED": "${MISSING}", "REMOTE": "prod-pass", "ODD": "odd"} {
		if got, ok := s.Value(Key{"project", "app", "dev"}, key); !ok || got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestTemplateFailures(t *testing.T) {
	cases := []string{
		`a: {$ref: missing}`,
		`a: {$ref: a}`,
		"a: {$ref: b}\nb: {$template: '${a}'}",
		`a: {$ref: project/app/base/password}`,
		`a: {$ref: outside/app/dev/password}`,
		`a: {$template: '${password | unknown}'}`,
		`a: {$template: '${password | base64 | json}'}`,
		`a: {$template: '${password |}'}`,
		`a: {$template: '${password'}`,
		`a: {$ref: '%ZZ'}`,
		`a: {$ref: '%2F'}`,
		`a: {$ref: '../password'}`,
		`a: {$ref: 'a/b'}`,
		`a: {$ref: 'API|TOKEN'}`,
		`a: {$ref: password, $template: text}`,
		`a: {$ref: password, $ref: password}`,
		`a: {$ref: 1}`,
		`a: {$template: [text]}`,
		`a: {$template: &x text}`,
		`a: !template text`,
		"password: null\na: {$ref: password}",
	}
	for _, plain := range cases {
		t.Run(plain, func(t *testing.T) {
			cfg := testutil.Config(t)
			_, err := NewBuilder(
				cfg,
				testutil.Plain{},
			).Build(t.Context(), "r", time.Now(), []source.Document{doc("base", "password: sensitive-canary"), doc("dev", plain)})
			if err == nil {
				t.Fatal("accepted invalid template")
			}
			if strings.Contains(err.Error(), "sensitive-canary") {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestReferenceEscaping(t *testing.T) {
	current := Key{"p", "c", "dev"}
	for text, want := range map[string]address{
		"a+b":                {current, "a+b"},
		"a%2520b":            {current, "a%20b"},
		"%20a%7Cb%7D":        {current, " a|b}"},
		"p/c/prod/%E9%9B%AA": {Key{"p", "c", "prod"}, "雪"},
	} {
		got, err := parseReference(current, text)
		if err != nil || got != want {
			t.Fatalf("%q: %v %v", text, got, err)
		}
	}
}

func TestEncoders(t *testing.T) {
	value := "a:b@ /?&=+雪\n\"<>\u2028"
	jsonValue, _ := json.Marshal(value)
	for encoder, want := range map[string]string{"urlquery": url.QueryEscape(value), "urlpath": url.PathEscape(value), "urluserinfo": url.User(value).String(), "json": string(jsonValue), "base64": base64.StdEncoding.EncodeToString([]byte(value))} {
		var got strings.Builder
		err := encode(value, encoder, func(s string) error { got.WriteString(s); return nil })
		if err != nil || got.String() != want {
			t.Fatalf("%s = %q, want %q (%v)", encoder, got.String(), want, err)
		}
	}
	large := strings.Repeat("abcde", 2000)
	var got strings.Builder
	if err := encode(
		large,
		"base64",
		func(s string) error { got.WriteString(s); return nil },
	); err != nil ||
		got.String() != base64.StdEncoding.EncodeToString([]byte(large)) {
		t.Fatal("chunked base64 mismatch")
	}
}

func TestTemplateLimits(t *testing.T) {
	for _, tt := range []struct {
		name, plain string
		change      func(*config.Limits)
		fail        bool
	}{
		{"depth boundary", "a: literal\nb: {$ref: a}\nc: {$ref: b}", func(l *config.Limits) { l.TemplateDepth = 2 }, false},
		{"cached depth", "a: literal\nb: {$ref: a}\nc: {$ref: b}", func(l *config.Limits) { l.TemplateDepth = 1 }, true},
		{"uncached depth", "a: {$ref: b}\nb: {$ref: c}\nc: literal", func(l *config.Limits) { l.TemplateDepth = 1 }, true},
		{"evaluation boundary", "a: x\nb: {$template: '${a}${a}'}", func(l *config.Limits) { l.TemplateEvaluations = 2 }, false},
		{"evaluation overflow", "a: x\nb: {$template: '${a}${a}'}", func(l *config.Limits) { l.TemplateEvaluations = 1 }, true},
		{"value boundary", "a: xx\nb: {$template: '${a}${a}'}", func(l *config.Limits) { l.TemplateValueBytes = 4 }, false},
		{"value overflow", "a: xx\nb: {$template: '${a}${a}'}", func(l *config.Limits) { l.TemplateValueBytes = 3 }, true},
		{"encoder overflow", "a: '::'\nb: {$template: '${a | urluserinfo}'}", func(l *config.Limits) { l.TemplateValueBytes = 5 }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testutil.Config(t)
			tt.change(&cfg.Limits)
			_, err := NewBuilder(
				cfg,
				testutil.Plain{},
			).Build(t.Context(), "r", time.Now(), []source.Document{doc("dev", tt.plain)})
			if (err != nil) != tt.fail {
				t.Fatalf("error = %v", err)
			}
		})
	}
	// Exercise aggregate expansion independently of input document accounting.
	raw := map[Key]map[string]scalar{
		{"project", "app", "dev"}: {
			"a": {kind: "$template", value: "prefix${b}"},
			"b": {value: "0123456789"},
		},
	}
	limits := config.Defaults().Limits
	limits.SnapshotBytes = 27
	if _, _, err := resolve(t.Context(), raw, limits); err == nil {
		t.Fatal("aggregate limit ignored")
	}
	limits.SnapshotBytes = 28
	if _, size, err := resolve(t.Context(), raw, limits); err != nil || size != 28 {
		t.Fatalf("aggregate boundary: %d %v", size, err)
	}
}

func TestTemplateScopeAndOverrides(t *testing.T) {
	cfg := testutil.Config(t)
	docs := []source.Document{
		doc("base", "password: base\nderived: {$ref: missing}\n"),
		doc("dev", "derived: literal\ncopy: {$ref: project/app/base/password}\n"),
	}
	if _, err := NewBuilder(
		cfg,
		testutil.Plain{},
	).Build(t.Context(), "r", time.Now(), docs); err == nil {
		t.Fatal("implicit base reference accepted")
	}
	// Explicit base is itself resolved, so discard its intentionally invalid descriptor.
	docs[0] = doc("base", "password: base\nderived: {$ref: password}\n")
	cfg.Scope.Environments = append(cfg.Scope.Environments, "base")
	s, err := NewBuilder(cfg, testutil.Plain{}).Build(t.Context(), "r", time.Now(), docs)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Value(Key{"project", "app", "dev"}, "derived"); got != "literal" {
		t.Fatal("descriptor override failed")
	}
	if got, _ := s.Value(Key{"project", "app", "dev"}, "copy"); got != "base" {
		t.Fatal("explicit base reference failed")
	}
	docs[1] = doc("dev", "derived: null\ncopy: {$ref: outside/app/dev/password}")
	docs = append(
		docs,
		source.Document{
			Project:     "outside",
			Config:      "app",
			Environment: "dev",
			Encrypted:   []byte("password: outside"),
		},
	)
	if _, err := NewBuilder(
		cfg,
		testutil.Plain{},
	).Build(t.Context(), "r", time.Now(), docs); err == nil {
		t.Fatal("outside project reference accepted")
	}
}

func TestReferenceRejectsInvalidUTF8(t *testing.T) {
	for _, text := range []string{"%FF", "%C0%AF", "%ED%A0%80", "project/%FF/dev/key"} {
		if _, err := parseReference(Key{"project", "app", "dev"}, text); err == nil {
			t.Errorf("accepted invalid UTF-8 reference %q", text)
		}
	}
}

func TestEmptyResolutionHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, raw := range []map[Key]map[string]scalar{nil, {{"project", "app", "dev"}: {}}} {
		if _, _, err := resolve(
			ctx,
			raw,
			config.Defaults().Limits,
		); !errors.Is(
			err,
			context.Canceled,
		) {
			t.Fatalf("error = %v, want cancellation", err)
		}
	}
}

func TestTemplateDiagnosticsAreBounded(t *testing.T) {
	key := Key{"project", "app", "dev"}
	values := make(map[string]scalar)
	for i := range 8 {
		name := fmt.Sprintf("key%d%s", i, strings.Repeat("x", 16384))
		next := fmt.Sprintf("key%d%s", i+1, strings.Repeat("x", 16384))
		values[name] = scalar{kind: "$ref", value: next}
	}
	_, _, err := resolve(
		t.Context(),
		map[Key]map[string]scalar{key: values},
		config.Defaults().Limits,
	)
	if err == nil {
		t.Fatal("missing target accepted")
	}
	if len(err.Error()) > 4096 {
		t.Fatalf("diagnostic exceeded bound: %d bytes", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "reference target unavailable") {
		t.Fatal("lost failure reason")
	}
}

func TestRejectTaggedOrAnchoredTemplateDocument(t *testing.T) {
	for _, plain := range []string{
		"!custom\nvalue: {$template: literal}\n",
		"&root\nvalue: {$template: literal}\n",
		"!!set\nvalue: null\n",
	} {
		if _, err := parse([]byte(plain)); err == nil {
			t.Errorf("accepted tagged or anchored root: %q", plain)
		}
	}
}

func TestResolutionErrorPreservesCause(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := resolver{ctx: ctx}
	_, err := r.value(address{Key{"project", "app", "dev"}, "value"}, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation cause: %v", err)
	}
}
