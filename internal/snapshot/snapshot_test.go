package snapshot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/decrypt"
	"github.com/zekihan/external-secrets-sops/internal/source"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func doc(env, plain string) source.Document {
	return source.Document{
		Project:     "project",
		Config:      "app",
		Environment: env,
		Encrypted:   []byte(plain),
	}
}
func TestSemantics(t *testing.T) {
	c := testutil.Config(t)
	c.Scope.Environments = []string{"dev", "base", "empty"}
	s, err := NewBuilder(
		c,
		testutil.Plain{},
	).Build(t.Context(), "revision", time.Now(), []source.Document{
		doc(
			"base",
			"inherit: base\noverride: old\ndelete: old\n",
		),
		doc(
			"dev",
			"override: new\ndelete: null\nabsent: null\nblank: ''\nnumber: 001\nboolean: true\n",
		),
		doc("empty", "{}\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	values, ok := s.Values(Key{"project", "app", "dev"})
	if !ok || values["inherit"] != "base" || values["override"] != "new" ||
		values["number"] != "001" ||
		values["boolean"] != "true" {
		t.Fatalf("bad merge: %v", values)
	}
	if _, ok = values["delete"]; ok {
		t.Fatal("null did not delete")
	}
	if v, ok := values["blank"]; !ok || v != "" {
		t.Fatal("empty value lost")
	}
	values["override"] = "mutated"
	if v, _ := s.Value(Key{"project", "app", "dev"}, "override"); v != "new" {
		t.Fatal("snapshot mutable")
	}
	if _, ok = s.Values(Key{"project", "app", "missing"}); ok {
		t.Fatal("implicit inheritance")
	}
	if v, _ := s.Value(Key{"project", "app", "empty"}, "inherit"); v != "base" {
		t.Fatal("empty document does not inherit")
	}
	if v, _ := s.Value(Key{"project", "app", "base"}, "delete"); v != "old" {
		t.Fatal("base mutated")
	}
}
func TestRejectInvalid(t *testing.T) {
	c := testutil.Config(t)
	for _, plain := range []string{"", "[]", "a: [b]", "a: {b: c}", "a: 1\na: 2", "a: &x test\nb: *x", "<<: {a: b}", "a: value\n---\nb: value", "a: !!binary YQ==", "'../x': value"} {
		if _, err := NewBuilder(
			c,
			testutil.Plain{},
		).Build(t.Context(), "r", time.Now(), []source.Document{doc("dev", plain)}); err == nil {
			t.Fatalf("accepted %q", plain)
		}
	}
	if _, err := NewBuilder(
		c,
		testutil.Plain{},
	).Build(t.Context(), "r", time.Now(), []source.Document{doc("base", "a: b")}); err == nil {
		t.Fatal("accepted no environments")
	}
	c.Limits.DocumentBytes = 4
	if _, err := NewBuilder(
		c,
		testutil.Plain{},
	).Build(t.Context(), "r", time.Now(), []source.Document{doc("dev", "a: long")}); err == nil {
		t.Fatal("size limit ignored")
	}
}
func TestConcurrentPublication(t *testing.T) {
	c := testutil.Config(t)
	builder := NewBuilder(c, testutil.Plain{})
	store := new(Store)
	create := func(rev string) *Snapshot {
		s, err := builder.Build(
			t.Context(),
			rev,
			time.Now(),
			[]source.Document{doc("dev", "a: "+rev+"\nb: "+rev)},
		)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	old, newer := create("old"), create("new")
	store.Publish(old)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				s := store.Load()
				values, _ := s.Values(Key{"project", "app", "dev"})
				if values["a"] != s.Revision() || values["b"] != s.Revision() {
					t.Error("mixed revision")
				}
			}
		})
	}
	for range 1000 {
		store.Publish(newer)
		store.Publish(old)
	}
	wg.Wait()
	if store.Ready(time.Now().Add(time.Hour), time.Minute) {
		t.Fatal("stale snapshot ready")
	}
	if !store.Ready(time.Now().Add(time.Hour), 0) {
		t.Fatal("availability default violated")
	}
}

type blocked struct {
	started chan struct{}
	release chan struct{}
}

func (b blocked) Decrypt(context.Context, []byte) ([]byte, error) {
	close(b.started)
	<-b.release
	return []byte("a: b"), nil
}
func TestExpiredCandidate(t *testing.T) {
	c := testutil.Config(t)
	d := blocked{make(chan struct{}), make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := NewBuilder(
			c,
			d,
		).Build(ctx, "r", time.Now(), []source.Document{doc("dev", "ignored")})
		done <- err
	}()
	<-d.started
	cancel()
	close(d.release)
	if err := <-done; err == nil {
		t.Fatal("expired candidate accepted")
	}
}
func BenchmarkSnapshotRead(b *testing.B) {
	c := testutil.Config(b)
	s, err := NewBuilder(
		c,
		testutil.Plain{},
	).Build(b.Context(), "r", time.Now(), []source.Document{doc("dev", "a: b")})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.Value(Key{"project", "app", "dev"}, "a")
	}
}
func BenchmarkCandidate(b *testing.B) {
	c := testutil.Config(b)
	var text strings.Builder
	for i := range 100 {
		fmt.Fprintf(&text, "key%d: '%s'\n", i, strings.Repeat("x", 128))
	}
	docs := make([]source.Document, 20)
	for i := range docs {
		docs[i] = source.Document{
			Project:     "project",
			Config:      fmt.Sprintf("app%d", i),
			Environment: "dev",
			Encrypted:   []byte(text.String()),
		}
	}
	builder := NewBuilder(c, testutil.Plain{})
	store := new(Store)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s, err := builder.Build(b.Context(), "r", time.Now(), docs)
		if err != nil {
			b.Fatal(err)
		}
		store.Publish(s)
	}
}

func BenchmarkEncryptedCandidate(b *testing.B) {
	c := testutil.Config(b)
	var plain strings.Builder
	for i := range 100 {
		fmt.Fprintf(&plain, "key%d: '%s'\n", i, strings.Repeat("x", 128))
	}
	encrypted := testutil.Encrypt(b, plain.String())
	docs := make([]source.Document, 20)
	for i := range docs {
		docs[i] = source.Document{
			Project:     "project",
			Config:      fmt.Sprintf("app%d", i),
			Environment: "dev",
			Encrypted:   encrypted,
		}
	}
	builder := NewBuilder(c, decrypt.SOPS{})
	store := new(Store)
	first, err := builder.Build(b.Context(), "old", time.Now(), docs)
	if err != nil {
		b.Fatal(err)
	}
	store.Publish(first)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		candidate, err := builder.Build(b.Context(), "new", time.Now(), docs)
		if err != nil {
			b.Fatal(err)
		}
		store.Publish(candidate)
	}
}

func TestEncryptedEmptyAndNull(t *testing.T) {
	c := testutil.Config(t)
	docs := []source.Document{doc("base", ""), doc("dev", "")}
	docs[0].Encrypted = testutil.Encrypt(t, "inherited: yes\nremoved: old\n")
	docs[1].Encrypted = testutil.Encrypt(t, "removed: null\nblank: ''\n")
	builder := NewBuilder(c, decrypt.SOPS{})
	s, err := builder.Build(t.Context(), "revision", time.Now(), docs)
	if err != nil {
		t.Fatal(err)
	}
	values, _ := s.Values(Key{"project", "app", "dev"})
	if _, ok := values["removed"]; ok {
		t.Fatal("encrypted null did not delete")
	}
	if v, ok := values["blank"]; !ok || v != "" {
		t.Fatal("encrypted empty string lost")
	}
	docs[1].Encrypted = testutil.Encrypt(t, "{}")
	s, err = builder.Build(t.Context(), "revision", time.Now(), docs)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Value(Key{"project", "app", "dev"}, "removed"); v != "old" {
		t.Fatal("encrypted empty document did not inherit")
	}
}
