package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
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
	sum := sha256.Sum256([]byte(value))
	for encoder, want := range map[string]string{"sha256": hex.EncodeToString(sum[:]), "urlquery": url.QueryEscape(value), "urlpath": url.PathEscape(value), "urluserinfo": url.User(value).String(), "json": string(jsonValue), "base64": base64.StdEncoding.EncodeToString([]byte(value))} {
		var got strings.Builder
		err := encode(
			t.Context(),
			value,
			encoder,
			func(s string) error { got.WriteString(s); return nil },
		)
		if err != nil || got.String() != want {
			t.Fatalf("%s = %q, want %q (%v)", encoder, got.String(), want, err)
		}
	}
	large := strings.Repeat("abcde", 2000)
	var got strings.Builder
	if err := encode(
		t.Context(),
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
		{"sha256 boundary", "a: x\nb: {$template: '${a | sha256}'}", func(l *config.Limits) { l.TemplateValueBytes = 64 }, false},
		{"sha256 overflow", "a: x\nb: {$template: '${a | sha256}'}", func(l *config.Limits) { l.TemplateValueBytes = 63 }, true},
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

func TestMissingReferencesDoNotExposeDescriptorContents(t *testing.T) {
	for _, plain := range []string{
		"output: {$ref: PRIVATE_REFERENCE_CANARY}",
		"output: {$template: '${PRIVATE_REFERENCE_CANARY}'}",
		"output: {$ref: project/PRIVATE_REFERENCE_CANARY/prod/key}",
		"output: {$ref: intermediate}\nintermediate: {$ref: PRIVATE_REFERENCE_CANARY}",
	} {
		cfg := testutil.Config(t)
		_, err := NewBuilder(
			cfg,
			testutil.Plain{},
		).Build(t.Context(), "r", time.Now(), []source.Document{doc("dev", plain)})
		if err == nil {
			t.Fatal("missing reference accepted")
		}
		if strings.Contains(err.Error(), "PRIVATE_REFERENCE_CANARY") {
			t.Fatal("missing reference disclosed descriptor contents")
		}
		if !strings.Contains(err.Error(), "reference target unavailable") {
			t.Fatal("failure reason missing")
		}
		if !strings.Contains(err.Error(), `"output"`) &&
			!strings.Contains(err.Error(), `"intermediate"`) {
			t.Fatal("source location missing")
		}
	}
}

func TestEncodersStopAtOutputLimit(t *testing.T) {
	for _, encoder := range []string{"base64", "json", "urlquery", "urlpath", "urluserinfo"} {
		t.Run(encoder, func(t *testing.T) {
			value := strings.Repeat("\"雪 /", 2048)
			sentinel := errors.New("output limit")
			size := 0
			failed := false
			err := encode(t.Context(), value, encoder, func(part string) error {
				if failed {
					t.Fatal("encoder continued after output limit")
				}
				if size+len(part) > 4096 {
					failed = true
					return sentinel
				}
				size += len(part)
				return nil
			})
			if !errors.Is(err, sentinel) {
				t.Fatalf("output limit not propagated: %v", err)
			}
		})
	}
}

func TestSHA256ACL(t *testing.T) {
	cfg := testutil.Config(t)
	s, err := NewBuilder(
		cfg,
		testutil.Plain{},
	).Build(t.Context(), "r", time.Now(), []source.Document{doc("dev", `PASSWORD: abc
ACL:
  $template: |
    user default off
    user app on #${PASSWORD | sha256} ~* +@all
`)})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Value(Key{"project", "app", "dev"}, "ACL")
	want := "user default off\nuser app on #ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad ~* +@all\n"
	if got != want {
		t.Fatalf("ACL = %q, want %q", got, want)
	}
	sentinel := errors.New("output limit")
	if err := encode(
		t.Context(),
		"abc",
		"sha256",
		func(string) error { return sentinel },
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatal("hash output limit not propagated")
	}
}

// checkBudgetContext makes excessive hash work fail deterministically, without a
// wall-clock assertion that varies across machines and race-test runs.
type checkBudgetContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int64
	budget int64
}

func (c *checkBudgetContext) Err() error {
	if c.checks.Add(1) > c.budget {
		c.cancel()
	}
	return c.Context.Err()
}

func boundedContext(t *testing.T, budget int64) *checkBudgetContext {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return &checkBudgetContext{Context: ctx, cancel: cancel, budget: budget}
}

func TestRepeatedLargeSHA256Target(t *testing.T) {
	const repetitions = 4096
	value := strings.Repeat("x", 1<<20)
	key := Key{"apps", "app", "cluster-1"}
	raw := map[Key]map[string]scalar{key: {
		"INPUT":  {value: value},
		"OUTPUT": {kind: "$template", value: strings.Repeat("${INPUT | sha256}", repetitions)},
	}}
	// Each output still pays reference and output checks. Rehashing the input for
	// every reference would exhaust this budget of context checks between chunks.
	ctx := boundedContext(t, repetitions*6+64)
	data, size, err := resolve(ctx, raw, config.Defaults().Limits)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(value))
	want := strings.Repeat(hex.EncodeToString(sum[:]), repetitions)
	if data[key]["OUTPUT"] != want {
		t.Fatal("repeated digest mismatch")
	}
	wantSize := int64(len("INPUT") + len(value) + len("OUTPUT") + len(want))
	if size != wantSize {
		t.Fatalf("resolved size = %d, want %d", size, wantSize)
	}
}

func TestSHA256CancellationBetweenChunks(t *testing.T) {
	ctx := boundedContext(t, 2)
	emitted := false
	err := encode(ctx, strings.Repeat("x", 128<<10), "sha256", func(string) error {
		emitted = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || emitted {
		t.Fatalf("cancellation = %v, emitted = %t", err, emitted)
	}
}

func TestSHA256MemoizationPreservesLimits(t *testing.T) {
	key := Key{"apps", "app", "cluster-1"}
	raw := map[Key]map[string]scalar{key: {
		"a": {value: "x"},
		"b": {kind: "$template", value: "${a | sha256}${a | sha256}"},
	}}
	for _, tc := range []struct {
		name   string
		change func(*config.Limits)
		fail   bool
	}{
		{"value boundary", func(l *config.Limits) { l.TemplateValueBytes = 128 }, false},
		{"value overflow", func(l *config.Limits) { l.TemplateValueBytes = 127 }, true},
		{"aggregate boundary", func(l *config.Limits) { l.SnapshotBytes = 131 }, false},
		{"aggregate overflow", func(l *config.Limits) { l.SnapshotBytes = 130 }, true},
		{"evaluation boundary", func(l *config.Limits) { l.TemplateEvaluations = 2 }, false},
		{"evaluation overflow", func(l *config.Limits) { l.TemplateEvaluations = 1 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := config.Defaults().Limits
			tc.change(&limits)
			_, _, err := resolve(t.Context(), raw, limits)
			if (err != nil) != tc.fail {
				t.Fatalf("resolution error = %v", err)
			}
		})
	}
	raw[key] = map[string]scalar{
		"a": {value: "x"},
		"b": {kind: "$ref", value: "a"},
		"c": {kind: "$template", value: "${b | sha256}"},
		"d": {kind: "$ref", value: "e"},
		"e": {kind: "$template", value: "${b | sha256}"},
	}
	limits := config.Defaults().Limits
	limits.TemplateDepth = 2
	if _, _, err := resolve(t.Context(), raw, limits); err == nil {
		t.Fatal("cached hash bypassed depth limit")
	}
	limits.TemplateDepth = 3
	if _, _, err := resolve(t.Context(), raw, limits); err != nil {
		t.Fatal(err)
	}
}

func TestSHA256CacheUsesFullResolvedAddress(t *testing.T) {
	key := Key{"apps", "app", "cluster-1"}
	sourceKey := Key{"shared", "source", "production"}
	r := resolver{
		ctx:    t.Context(),
		limits: config.Defaults().Limits,
		raw: map[Key]map[string]scalar{
			key: {
				"INPUT": {value: "first"},
				"OUTPUT": {
					kind:  "$template",
					value: "${INPUT | sha256}${shared/source/production/INPUT | sha256}${INPUT | sha256}",
				},
			},
			sourceKey: {"INPUT": {value: "second"}},
		},
		memo:     make(map[address]resolved),
		digests:  make(map[address]string),
		visiting: make(map[address]bool),
	}
	result, err := r.value(address{key, "OUTPUT"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, second := sha256.Sum256([]byte("first")), sha256.Sum256([]byte("second"))
	want := hex.EncodeToString(
		first[:],
	) + hex.EncodeToString(
		second[:],
	) + hex.EncodeToString(
		first[:],
	)
	if result.value != want || len(r.digests) != 2 || r.evaluations != 3 {
		t.Fatal("hash cache crossed addresses or skipped accounting")
	}
}
