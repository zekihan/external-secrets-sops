package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/zekihan/external-secrets-sops/internal/decrypt"
	"github.com/zekihan/external-secrets-sops/internal/source"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

type countingSOPS struct {
	decrypt.SOPS
	calls atomic.Int32
}

func (d *countingSOPS) Decrypt(ctx context.Context, data []byte) ([]byte, error) {
	d.calls.Add(1)
	return d.SOPS.Decrypt(ctx, data)
}

func TestCandidatePolicyBeforeAnyDecryption(t *testing.T) {
	cfg := testutil.Config(t)
	d := &countingSOPS{SOPS: decrypt.SOPS{CandidateRecipients: 256}}
	builder := NewBuilder(cfg, d)
	encrypted := testutil.Encrypt(t, "value: fixture\n")
	valid := doc("dev", "")
	valid.Encrypted = encrypted
	active, err := builder.Build(t.Context(), "old", time.Now(), []source.Document{valid})
	if err != nil || d.calls.Load() != 1 {
		t.Fatalf("supported candidate failed: %v", err)
	}
	store := new(Store)
	store.Publish(active)
	for _, count := range []int{2, 257} {
		t.Run(fmt.Sprintf("documents=%d", count), func(t *testing.T) {
			d.calls.Store(0)
			docs := make([]source.Document, count)
			for i := range docs {
				docs[i] = valid
				docs[i].Config = fmt.Sprintf("app%d", i)
			}
			if count == 2 {
				// Even a disallowed backend in the last document must reject
				// the whole candidate before the first document dispatches.
				docs[1].Encrypted = bytes.Replace(encrypted, []byte("sops:\n"), []byte(
					"sops:\n  hc_vault:\n    - vault_address: https://fixture.invalid\n"+
						"      engine_path: transit\n      key_name: fixture\n"+
						"      created_at: 2026-01-01T00:00:00Z\n      enc: fixture\n",
				), 1)
			}
			candidate, buildErr := builder.Build(t.Context(), "rejected", time.Now(), docs)
			if buildErr == nil || candidate != nil || d.calls.Load() != 0 {
				t.Fatal("candidate policy did not prevent key recovery")
			}
			if store.Load() != active || store.Load().Revision() != "old" {
				t.Fatal("rejected candidate changed the active snapshot")
			}
		})
	}
}

func TestDualRecipientCandidateBeyondFormerBudget(t *testing.T) {
	cfg := testutil.Config(t)
	next, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rotated := testutil.EncryptRecipients(
		t,
		"value: fixture\n",
		next.Recipient().String(),
		testutil.FixtureRecipient,
	)
	// 137 documents (the cluster-1 scope that motivated OPS-483) plus growth,
	// two recipients each: 300 entries exceed the former fixed budget of 256.
	docs := make([]source.Document, 150)
	for i := range docs {
		docs[i] = doc("dev", "")
		docs[i].Config = fmt.Sprintf("app%d", i)
		docs[i].Encrypted = rotated
	}
	builder := NewBuilder(
		cfg,
		decrypt.SOPS{CandidateRecipients: cfg.Limits.CandidateRecipients},
	)
	candidate, err := builder.Build(t.Context(), "rotated", time.Now(), docs)
	if err != nil || candidate.Count() != len(docs) {
		t.Fatalf("dual-recipient candidate rejected: %v", err)
	}
	if v, ok := candidate.Value(
		Key{Project: "project", Config: "app149", Environment: "dev"},
		"value",
	); !ok || v != "fixture" {
		t.Fatal("rotated document value missing")
	}
	cfg.Limits.CandidateRecipients = 2*len(docs) - 1
	builder = NewBuilder(
		cfg,
		decrypt.SOPS{CandidateRecipients: cfg.Limits.CandidateRecipients},
	)
	if _, err = builder.Build(t.Context(), "rejected", time.Now(), docs); err == nil {
		t.Fatal("candidate above configured budget accepted")
	}
}
