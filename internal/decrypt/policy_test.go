package decrypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sopsconfig "github.com/getsops/sops/v3/config"
	yamlstore "github.com/getsops/sops/v3/stores/yaml"
	"go.yaml.in/yaml/v3"

	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func fixtureMetadata(t *testing.T) map[string]any {
	t.Helper()
	var fixture map[string]any
	if err := yaml.Unmarshal(
		testutil.Encrypt(t, "canary: fixture-only-value\n"),
		&fixture,
	); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func encodeFixture(t *testing.T, fixture map[string]any) []byte {
	t.Helper()
	b, err := yaml.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRemoteBackendsRejectedBeforeRecovery(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	t.Setenv("VAULT_TOKEN", "synthetic-fixture-token")
	backends := map[string]map[string]any{
		"hc_vault": {"vault_address": server.URL, "engine_path": "transit", "key_name": "fixture"},
		"azure_kv": {"vault_url": server.URL, "name": "fixture", "version": "1"},
		"kms":      {"arn": "arn:aws:kms:us-east-1:111122223333:key/fixture"},
		"gcp_kms": {
			"resource_id": "projects/fixture/locations/global/keyRings/fixture/cryptoKeys/fixture",
		},
		"hckms": {"key_id": "fixture:fixture"},
	}
	for backend, recipient := range backends {
		for _, layout := range []string{"flat", "grouped", "mixed", "uppercase"} {
			t.Run(backend+"/"+layout, func(t *testing.T) {
				fixture := fixtureMetadata(t)
				meta := fixture["sops"].(map[string]any)
				recipient["created_at"] = "2026-01-01T00:00:00Z"
				recipient["enc"] = "synthetic-ciphertext"
				// Include a usable local recipient: policy must inspect every
				// fallback rather than stop once a local key can succeed.
				if layout == "grouped" || layout == "mixed" {
					meta["key_groups"] = []any{map[string]any{
						backend: []any{recipient}, "age": meta["age"],
					}}
					if layout == "grouped" {
						delete(meta, "age")
					}
				} else {
					name := backend
					if layout == "uppercase" {
						name = strings.ToUpper(name)
					}
					meta[name] = []any{recipient}
				}
				encrypted := encodeFixture(t, fixture)
				// The envelope must be parseable by upstream SOPS; otherwise
				// this could pass by only testing malformed YAML or dates.
				store := yamlstore.NewStore(&sopsconfig.YAMLStoreConfig{})
				if _, err := store.LoadEncryptedFile(encrypted); err != nil {
					t.Fatalf("invalid synthetic backend fixture: %v", err)
				}
				if plain, err := (SOPS{}).Decrypt(
					t.Context(),
					encrypted,
				); err == nil ||
					plain != nil {
					t.Fatal("remote metadata accepted")
				} else if !strings.Contains(
					err.Error(),
					"metadata violates",
				) {
					t.Fatalf("expected preflight rejection, got %v", err)
				}
			})
		}
	}
	if calls.Load() != 0 {
		t.Fatal("metadata triggered a remote key recovery request")
	}
}

func TestRecipientAndGroupBudgets(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		for _, count := range []int{maxDocumentRecipients, maxDocumentRecipients + 1} {
			t.Run(fmt.Sprintf("grouped=%v/recipients=%d", grouped, count), func(t *testing.T) {
				fixture := fixtureMetadata(t)
				meta := fixture["sops"].(map[string]any)
				key := meta["age"].([]any)[0]
				keys := make([]any, count)
				for i := range keys {
					keys[i] = key
				}
				meta["age"] = keys
				if grouped {
					meta["key_groups"] = []any{map[string]any{"age": keys}}
					delete(meta, "age")
				}
				plain, err := (SOPS{}).Decrypt(t.Context(), encodeFixture(t, fixture))
				if count == maxDocumentRecipients {
					if err != nil || !bytes.Contains(plain, []byte("fixture-only-value")) {
						t.Fatalf("supported boundary rejected: %v", err)
					}
				} else if err == nil || plain != nil {
					t.Fatal("excess recipients accepted")
				}
			})
		}
	}
	for _, count := range []int{maxKeyGroups, maxKeyGroups + 1} {
		fixture := fixtureMetadata(t)
		meta := fixture["sops"].(map[string]any)
		groups := make([]any, count)
		for i := range groups {
			groups[i] = map[string]any{"age": meta["age"]}
		}
		delete(meta, "age")
		meta["key_groups"] = groups
		err := (SOPS{}).ValidateCandidate(t.Context(), [][]byte{encodeFixture(t, fixture)})
		if (err == nil) != (count == maxKeyGroups) {
			t.Fatalf("unexpected policy result for %d groups: %v", count, err)
		}
	}
	// The document limit is aggregate, not a separate allowance per group.
	fixtureWithGroups := fixtureMetadata(t)
	meta := fixtureWithGroups["sops"].(map[string]any)
	keys := make([]any, maxDocumentRecipients/2+1)
	for i := range keys {
		keys[i] = meta["age"].([]any)[0]
	}
	delete(meta, "age")
	meta["key_groups"] = []any{map[string]any{"age": keys}, map[string]any{"age": keys}}
	if err := (SOPS{}).ValidateCandidate(
		t.Context(),
		[][]byte{encodeFixture(t, fixtureWithGroups)},
	); err == nil {
		t.Fatal("aggregate recipients across groups exceeded the document budget")
	}
	fixture := testutil.Encrypt(t, "value: fixture\n")
	documents := make([][]byte, maxCandidateRecipients+1)
	for i := range documents {
		documents[i] = fixture
	}
	if err := (SOPS{}).ValidateCandidate(
		t.Context(),
		documents[:maxCandidateRecipients],
	); err != nil {
		t.Fatalf("candidate boundary rejected: %v", err)
	}
	if err := (SOPS{}).ValidateCandidate(t.Context(), documents); err == nil {
		t.Fatal("excess candidate work accepted")
	}
}

func TestLocalShamirAndIntegrity(t *testing.T) {
	for _, count := range []int{2, maxKeyGroups} {
		encrypted := testutil.EncryptGroups(t, "canary: fixture-only-value\n", count)
		plain, err := (SOPS{}).Decrypt(t.Context(), encrypted)
		if err != nil || !bytes.Contains(plain, []byte("fixture-only-value")) {
			t.Fatalf("supported Shamir document with %d groups failed: %v", count, err)
		}
		tampered := bytes.Replace(encrypted, []byte("canary:"), []byte("changed:"), 1)
		if plain, err = (SOPS{}).Decrypt(t.Context(), tampered); err == nil || plain != nil {
			t.Fatal("grouped metadata bypassed MAC verification")
		}
	}
}

func TestMetadataAmbiguityAndPluginsRejected(t *testing.T) {
	encrypted := testutil.Encrypt(t, "value: fixture\n")
	for name, data := range map[string][]byte{
		"duplicate":      append(bytes.Clone(encrypted), []byte("sops: {}\n")...),
		"multiple":       append(bytes.Clone(encrypted), append([]byte("---\n"), encrypted...)...),
		"alias":          []byte("sops: &meta {age: []}\nvalue: *meta\n"),
		"merge":          []byte("sops: {<<: {age: []}}\n"),
		"case collision": []byte("sops: {age: [], AGE: [], lastmodified: '2026-01-01T00:00:00Z'}\n"),
		"nested":         []byte("sops: " + strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := (SOPS{}).Decrypt(t.Context(), data); err == nil {
				t.Fatal("ambiguous metadata accepted")
			}
		})
	}
	fixture := fixtureMetadata(t)
	meta := fixture["sops"].(map[string]any)
	meta["age"].([]any)[0].(map[string]any)["recipient"] = "age1fixture1plugin"
	if _, err := (SOPS{}).Decrypt(t.Context(), encodeFixture(t, fixture)); err == nil {
		t.Fatal("Age plugin recipient accepted")
	}
	// SOPS uses a case-insensitive metadata decoder; preflight must inspect
	// the interpreted backend rather than only matching raw lowercase keys.
	meta["AGE"] = meta["age"]
	delete(meta, "age")
	if _, err := (SOPS{}).Decrypt(t.Context(), encodeFixture(t, fixture)); err == nil {
		t.Fatal("case variation bypassed recipient policy")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (SOPS{}).ValidateCandidate(
		ctx,
		[][]byte{encrypted},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal("candidate validation ignored cancellation")
	}
}
