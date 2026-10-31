package app

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/httpapi"
	"github.com/zekihan/external-secrets-sops/internal/snapshot"
	"github.com/zekihan/external-secrets-sops/internal/source"
	"github.com/zekihan/external-secrets-sops/internal/syncer"
	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

type transportWorker struct{}

func (transportWorker) Trigger()              {}
func (transportWorker) Status() syncer.Status { return syncer.Status{} }

func testTLSFiles(t *testing.T) (config.Server, *http.Client) {
	t.Helper()
	// Reuse net/http's synthetic test certificate; no external credentials or trust changes.
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer fixture.Close()
	certificate := fixture.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := config.Server{
		Address:     "127.0.0.1:0",
		TLSCertFile: filepath.Join(dir, "cert.pem"),
		TLSKeyFile:  filepath.Join(dir, "key.pem"),
	}
	if err := os.WriteFile(
		cfg.TLSCertFile,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}),
		0600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		cfg.TLSKeyFile,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
		0600,
	); err != nil {
		t.Fatal(err)
	}
	client := fixture.Client()
	client.Timeout = 3 * time.Second
	t.Cleanup(client.CloseIdleConnections)
	return cfg, client
}

func TestListenRejectsUnsafeTransport(t *testing.T) {
	for _, address := range []string{":0", "0.0.0.0:0", "[::]:0", "192.0.2.1:0", "localhost:0"} {
		t.Run(address, func(t *testing.T) {
			// Run must enforce the same policy when a caller bypasses config.Load.
			cfg := config.Defaults()
			cfg.Server.Address = address
			if err := Run(
				t.Context(),
				cfg,
				config.Credentials{},
				testutil.Logger(),
			); err == nil ||
				!strings.Contains(err.Error(), "requires") {
				t.Fatalf("unsafe Run error = %v", err)
			}
		})
	}
	cfg, _ := testTLSFiles(t)
	for _, bad := range []config.Server{
		{Address: cfg.Address, TLSCertFile: cfg.TLSCertFile},
		{Address: cfg.Address, TLSKeyFile: cfg.TLSKeyFile},
		{Address: cfg.Address, TLSCertFile: cfg.TLSCertFile, TLSKeyFile: filepath.Join(t.TempDir(), "missing")},
		{Address: cfg.Address, TLSCertFile: cfg.TLSKeyFile, TLSKeyFile: cfg.TLSKeyFile},
	} {
		listener, err := listen(bad)
		if err == nil {
			listener.Close()
			t.Fatal("accepted invalid TLS configuration")
		}
		if strings.Contains(err.Error(), cfg.TLSKeyFile) {
			t.Fatal("error exposed private key path")
		}
	}
}

func TestListenerTransportPreservesAPIControls(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		t.Run(map[bool]string{false: "loopback HTTP", true: "TLS"}[useTLS], func(t *testing.T) {
			cfg := testutil.Config(t)
			store := new(snapshot.Store)
			candidate, err := snapshot.NewBuilder(cfg, testutil.Plain{}).
				Build(t.Context(), strings.Repeat("a", 40), time.Now(), []source.Document{
					{
						Project:     "project",
						Config:      "app",
						Environment: "dev",
						Encrypted:   []byte("key: synthetic-value"),
					},
				})
			if err != nil {
				t.Fatal(err)
			}
			store.Publish(candidate)
			serverCfg := config.Server{Address: "127.0.0.1:0"}
			client := &http.Client{Timeout: 3 * time.Second}
			scheme := "http://"
			if useTLS {
				serverCfg, client = testTLSFiles(t)
				scheme = "https://"
			}
			listener, err := listen(serverCfg)
			if err != nil {
				t.Fatal(err)
			}
			api := httpapi.New(
				store,
				transportWorker{},
				nil,
				config.Credentials{Reader: "reader", Admin: "admin"},
				"main",
				time.Hour,
				testutil.Logger(),
			)
			server := httptest.NewUnstartedServer(api.Handler())
			server.Listener.Close()
			server.Listener = listener
			server.Start()
			defer server.Close()
			baseURL := scheme + listener.Addr().String()
			for _, tt := range []struct {
				path, token string
				status      int
				body        string
			}{
				{"/livez", "", 200, ""}, {"/readyz", "", 200, ""},
				{"/v1/secrets/project/app/dev/key", "", 401, ""},
				{"/v1/secrets/project/app/dev/key", "wrong", 401, ""},
				{"/v1/secrets/project/app/dev/key", "admin", 403, ""},
				{"/v1/secrets/project/app/dev/key", "reader", 200, "{\"value\":\"synthetic-value\"}\n"},
				{"/v1/status", "reader", 403, ""}, {"/v1/status", "admin", 200, ""},
			} {
				req, err := http.NewRequestWithContext(
					t.Context(),
					"GET",
					baseURL+tt.path,
					http.NoBody,
				)
				if err != nil {
					t.Fatal(err)
				}
				if tt.token != "" {
					req.Header.Set("Authorization", "Bearer "+tt.token)
				}
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != tt.status || (tt.body != "" && string(body) != tt.body) {
					t.Fatalf("%s %s: %d %s", tt.path, tt.token, response.StatusCode, body)
				}
				if response.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("missing cache prevention")
				}
				if tt.body != "" &&
					response.Header.Get("X-Secret-Revision") != candidate.Revision() {
					t.Fatal("revision integrity lost")
				}
			}
			store.Publish(candidate.Renew(time.Now().Add(-2 * time.Hour)))
			for _, path := range []string{"/readyz", "/v1/secrets/project/app/dev/key"} {
				req, _ := http.NewRequestWithContext(t.Context(), "GET", baseURL+path, http.NoBody)
				req.Header.Set("Authorization", "Bearer reader")
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != 503 {
					t.Fatal("served stale snapshot")
				}
			}
			if useTLS {
				req, _ := http.NewRequestWithContext(
					t.Context(),
					"GET",
					"http://"+listener.Addr().String()+"/v1/secrets/project/app/dev/key",
					http.NoBody,
				)
				req.Header.Set("Authorization", "Bearer reader")
				response, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != http.StatusBadRequest {
					t.Fatal("plaintext reached API")
				}
				transport := client.Transport.(*http.Transport).Clone()
				transport.TLSClientConfig.MinVersion = tls.VersionTLS11
				transport.TLSClientConfig.MaxVersion = tls.VersionTLS11
				oldClient := &http.Client{Transport: transport, Timeout: 3 * time.Second}
				defer oldClient.CloseIdleConnections()
				response, err = oldClient.Get(baseURL + "/livez")
				if err == nil {
					response.Body.Close()
					t.Fatal("accepted TLS before 1.2")
				}
			}
		})
	}
}
