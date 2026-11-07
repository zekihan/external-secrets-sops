package certs

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func volume(t *testing.T, ca *testutil.CA, serial int64) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	certPEM, keyPEM := ca.Issue(t, serial)
	testutil.WriteSecretVolume(t, dir, map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM})
	return dir, filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
}

func serial(t *testing.T, r *Reloader) int64 {
	t.Helper()
	certificate, err := r.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	return certificate.Leaf.SerialNumber.Int64()
}

func TestNewRejectsInvalidPairs(t *testing.T) {
	ca := testutil.NewCA(t)
	_, certFile, keyFile := volume(t, ca, 2)
	for name, files := range map[string][2]string{
		"missing key":  {certFile, filepath.Join(t.TempDir(), "missing")},
		"swapped":      {keyFile, certFile},
		"cert as key":  {certFile, certFile},
		"missing cert": {filepath.Join(t.TempDir(), "missing"), keyFile},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(files[0], files[1], testutil.Logger()); err == nil {
				t.Fatal("accepted invalid pair")
			}
		})
	}
	_, otherCert, _ := volume(t, ca, 3)
	if _, err := New(otherCert, keyFile, testutil.Logger()); err == nil {
		t.Fatal("accepted mismatched pair")
	}
}

func TestReloadSwapsVolumeAndKeepsLastGood(t *testing.T) {
	ca := testutil.NewCA(t)
	dir, certFile, keyFile := volume(t, ca, 10)
	r, err := New(certFile, keyFile, testutil.Logger())
	if err != nil {
		t.Fatal(err)
	}
	r.Reload(t.Context())
	if got := serial(t, r); got != 10 || r.Status().Successes != 0 {
		t.Fatalf("unchanged files reloaded: serial %d, %+v", got, r.Status())
	}

	certPEM, keyPEM := ca.Issue(t, 11)
	testutil.WriteSecretVolume(t, dir, map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM})
	r.Reload(t.Context())
	if got := serial(t, r); got != 11 {
		t.Fatalf("serial after rotation = %d", got)
	}

	// A mismatched pair, as seen mid-replacement, keeps the previous certificate.
	otherCert, _ := ca.Issue(t, 12)
	testutil.WriteSecretVolume(t, dir, map[string][]byte{"tls.crt": otherCert, "tls.key": keyPEM})
	r.Reload(t.Context())
	r.Reload(t.Context())
	status := r.Status()
	if got := serial(t, r); got != 11 || status.Failures != 1 || !status.Failing {
		t.Fatalf("invalid pair: serial %d, %+v", got, status)
	}

	// Garbage and missing files are distinct failures that also keep serving.
	testutil.WriteSecretVolume(
		t,
		dir,
		map[string][]byte{"tls.crt": []byte("junk"), "tls.key": keyPEM},
	)
	r.Reload(t.Context())
	if err := os.Remove(certFile); err != nil {
		t.Fatal(err)
	}
	r.Reload(t.Context())
	r.Reload(t.Context())
	if got, status := serial(t, r), r.Status(); got != 11 || status.Failures != 3 {
		t.Fatalf("failures: serial %d, %+v", got, status)
	}

	certPEM, keyPEM = ca.Issue(t, 13)
	if err := os.Symlink(filepath.Join("..data", "tls.crt"), certFile); err != nil {
		t.Fatal(err)
	}
	testutil.WriteSecretVolume(t, dir, map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM})
	r.Reload(t.Context())
	status = r.Status()
	if got := serial(t, r); got != 13 || status.Failing || status.Successes != 2 ||
		status.SerialHex != "d" {
		t.Fatalf("recovery: serial %d, %+v", got, status)
	}
}

func TestReloadRejectsOversizedFiles(t *testing.T) {
	ca := testutil.NewCA(t)
	dir, certFile, keyFile := volume(t, ca, 20)
	r, err := New(certFile, keyFile, testutil.Logger())
	if err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM := ca.Issue(t, 21)
	padded := append(make([]byte, maxFileBytes), certPEM...)
	testutil.WriteSecretVolume(t, dir, map[string][]byte{"tls.crt": padded, "tls.key": keyPEM})
	r.Reload(t.Context())
	if got := serial(t, r); got != 20 || r.Status().Failures != 1 {
		t.Fatalf("oversized: serial %d, %+v", got, r.Status())
	}
}
