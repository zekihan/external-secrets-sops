package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// CA issues synthetic loopback server certificates for TLS tests.
type CA struct {
	Pool        *x509.CertPool
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
}

func NewCA(t testing.TB) *CA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "synthetic test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	return &CA{Pool: pool, certificate: certificate, key: key}
}

// Issue returns PEM certificate and PKCS#8 key bytes for 127.0.0.1 with the given serial.
func (ca *CA) Issue(t testing.TB, serial int64) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		ca.certificate,
		&key.PublicKey,
		ca.key,
	)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
}

// WriteSecretVolume mimics the kubelet atomic writer: files live in a timestamped
// directory, "..data" points at it, and each visible name links through "..data".
// Successive calls swap "..data" with a single rename and remove the old directory.
func WriteSecretVolume(t testing.TB, dir string, files map[string][]byte) {
	t.Helper()
	dataLink := filepath.Join(dir, "..data")
	previous, _ := os.Readlink(dataLink)
	generation := fmt.Sprintf("..%d", time.Now().UnixNano())
	if err := os.Mkdir(filepath.Join(dir, generation), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, generation, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tmp := filepath.Join(dir, "..data_tmp")
	if err := os.Symlink(generation, tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, dataLink); err != nil {
		t.Fatal(err)
	}
	for name := range files {
		link := filepath.Join(dir, name)
		if _, err := os.Lstat(link); err == nil {
			continue
		}
		if err := os.Symlink(filepath.Join("..data", name), link); err != nil {
			t.Fatal(err)
		}
	}
	if previous != "" {
		if err := os.RemoveAll(filepath.Join(dir, previous)); err != nil {
			t.Fatal(err)
		}
	}
}
