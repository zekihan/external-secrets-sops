// Package certs serves TLS certificates that are replaced on disk without a restart.
package certs

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// maxFileBytes bounds each PEM read; certificate chains and keys are far smaller.
const maxFileBytes = 1 << 20

// Status describes the active certificate and reload outcomes for one process.
type Status struct {
	Successes   uint64    `json:"successes"`
	Failures    uint64    `json:"failures"`
	Failing     bool      `json:"failing"`
	LoadedAt    time.Time `json:"loadedAt"`
	NotAfter    time.Time `json:"notAfter"`
	SerialHex   string    `json:"serial"`
	LastAttempt time.Time `json:"lastAttempt"`
}

type active struct {
	certificate *tls.Certificate
	digest      [sha256.Size]byte
	loadedAt    time.Time
}

// Reloader polls a certificate/key pair and atomically swaps in each valid change.
// Content hashing (rather than mtime or inode checks) follows Kubernetes Secret volume
// "..data" symlink swaps and in-place rewrites alike. Invalid or partially written
// pairs are rejected; the last valid pair keeps serving.
type Reloader struct {
	certFile, keyFile string
	logger            *slog.Logger

	current   atomic.Pointer[active]
	successes atomic.Uint64
	failures  atomic.Uint64

	mu          sync.Mutex
	failing     bool
	failed      [sha256.Size]byte
	lastAttempt time.Time
}

// New loads the initial pair; startup fails rather than serving without a certificate.
func New(certFile, keyFile string, logger *slog.Logger) (*Reloader, error) {
	r := &Reloader{certFile: certFile, keyFile: keyFile, logger: logger}
	certPEM, keyPEM, err := r.read()
	if err != nil {
		return nil, errors.New("cannot load server TLS certificate and key")
	}
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("cannot load server TLS certificate and key")
	}
	r.current.Store(&active{
		certificate: &certificate,
		digest:      digest(certPEM, keyPEM),
		loadedAt:    time.Now(),
	})
	return r, nil
}

// GetCertificate implements tls.Config.GetCertificate.
func (r *Reloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return r.current.Load().certificate, nil
}

// Run checks for changes every interval until ctx ends.
func (r *Reloader) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Reload(ctx)
		}
	}
}

// Reload reads the files once and publishes them if they changed and are valid.
// Each distinct failure is counted and logged once; retries continue every call.
func (r *Reloader) Reload(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastAttempt = time.Now()
	certPEM, keyPEM, err := r.read()
	if err != nil {
		r.fail(ctx, [sha256.Size]byte{}, "read")
		return
	}
	sum := digest(certPEM, keyPEM)
	if sum == r.current.Load().digest {
		r.failing = false
		return
	}
	if r.failing && sum == r.failed {
		return
	}
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		// Also covers a read that straddled a non-atomic cert/key replacement.
		r.fail(ctx, sum, "parse")
		return
	}
	r.current.Store(&active{certificate: &certificate, digest: sum, loadedAt: time.Now()})
	r.failing = false
	r.successes.Add(1)
	r.logger.InfoContext(
		ctx,
		"TLS certificate reloaded",
		slog.String("serial", certificate.Leaf.SerialNumber.Text(16)),
		slog.Time("not_after", certificate.Leaf.NotAfter),
	)
}

func (r *Reloader) fail(ctx context.Context, sum [sha256.Size]byte, stage string) {
	if r.failing && sum == r.failed {
		return
	}
	r.failing, r.failed = true, sum
	r.failures.Add(1)
	// Errors are not logged verbatim: they can contain credential file paths.
	r.logger.WarnContext(
		ctx,
		"TLS certificate reload failed; keeping previous certificate",
		slog.String("stage", stage),
	)
}

// Status reports the active certificate and reload counters.
func (r *Reloader) Status() Status {
	current := r.current.Load()
	r.mu.Lock()
	failing, lastAttempt := r.failing, r.lastAttempt
	r.mu.Unlock()
	return Status{
		Successes:   r.successes.Load(),
		Failures:    r.failures.Load(),
		Failing:     failing,
		LoadedAt:    current.loadedAt,
		NotAfter:    current.certificate.Leaf.NotAfter,
		SerialHex:   current.certificate.Leaf.SerialNumber.Text(16),
		LastAttempt: lastAttempt,
	}
}

func (r *Reloader) read() ([]byte, []byte, error) {
	certPEM, err := readFile(r.certFile)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := readFile(r.keyFile)
	if err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFileBytes {
		return nil, errors.New("file too large")
	}
	return b, nil
}

func digest(certPEM, keyPEM []byte) [sha256.Size]byte {
	h := sha256.New()
	h.Write(certPEM)
	h.Write([]byte{0})
	h.Write(keyPEM)
	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	return sum
}
