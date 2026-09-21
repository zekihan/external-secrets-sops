package decrypt

import (
	"bytes"
	"context"
	"os"
	"testing"

	"filippo.io/age"
	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func TestAgeAndIntegrity(t *testing.T) {
	encrypted := testutil.Encrypt(t, "canary: fixture-only-value\n")
	plain, err := (SOPS{}).Decrypt(t.Context(), encrypted)
	if err != nil || !bytes.Contains(plain, []byte("fixture-only-value")) {
		t.Fatal("Age decrypt failed")
	}
	tampered := bytes.Replace(encrypted, []byte("canary:"), []byte("changed:"), 1)
	if _, err = (SOPS{}).Decrypt(t.Context(), tampered); err == nil {
		t.Fatal("tampering accepted")
	}
	t.Setenv("SOPS_AGE_KEY_FILE", t.TempDir()+"/missing")
	t.Setenv("SOPS_AGE_KEY", "")
	t.Setenv("SOPS_AGE_KEY_CMD", "")
	if err = os.Unsetenv("SOPS_AGE_KEY_CMD"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err = (SOPS{}).Decrypt(t.Context(), encrypted); err == nil {
		t.Fatal("missing key accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = (SOPS{}).Decrypt(ctx, encrypted); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err = (SOPS{}).Decrypt(t.Context(), []byte("plain: text")); err == nil {
		t.Fatal("plaintext accepted")
	}
}
func TestPGP(t *testing.T) {
	t.Setenv("GNUPGHOME", t.TempDir())
	t.Setenv("SOPS_PGP_FP", "")
	// SOPS' in-process PGP reader accepts an explicitly configured private keyring.
	key, err := os.ReadFile("../../testdata/pgp/private.asc")
	if err != nil {
		t.Fatal(err)
	}
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(key))
	if err != nil {
		t.Fatal(err)
	}
	var ring bytes.Buffer
	for _, entity := range entities {
		if err = entity.SerializePrivate(&ring, nil); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	if err = os.WriteFile(dir+"/secring.gpg", ring.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOPS_GPG_EXEC", "/nonexistent")
	t.Setenv("GNUPGHOME", dir)
	encrypted, err := os.ReadFile("../../testdata/pgp/values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := (SOPS{}).Decrypt(t.Context(), encrypted)
	if err != nil || !bytes.Contains(plain, []byte("pgp-fixture")) {
		t.Fatalf("PGP fixture failed: %v", err)
	}
}

func TestWrongAgeIdentity(t *testing.T) {
	encrypted := testutil.Encrypt(t, "value: protected")
	wrong, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	keyFile := t.TempDir() + "/age.txt"
	if err = os.WriteFile(keyFile, []byte(wrong.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
	t.Setenv("SOPS_AGE_KEY", wrong.String())
	t.Setenv("SOPS_AGE_KEY_CMD", "")
	if err = os.Unsetenv("SOPS_AGE_KEY_CMD"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err = (SOPS{}).Decrypt(t.Context(), encrypted); err == nil {
		t.Fatal("wrong Age identity accepted")
	}
}
func TestBackendPanicIsContained(t *testing.T) {
	encrypted := testutil.Encrypt(t, "value: protected")
	t.Setenv("SOPS_AGE_KEY_CMD", "")
	if _, err := (SOPS{}).Decrypt(t.Context(), encrypted); err == nil {
		t.Fatal("broken key command accepted")
	}
}
