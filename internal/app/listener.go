package app

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net"

	"github.com/zekihan/external-secrets-sops/internal/certs"
	"github.com/zekihan/external-secrets-sops/internal/config"
)

// listen enforces transport policy even when Run is called without config.Load.
// The returned reloader is nil for loopback HTTP listeners.
func listen(cfg config.Server, logger *slog.Logger) (net.Listener, *certs.Reloader, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	var reloader *certs.Reloader
	if cfg.TLSCertFile != "" {
		var err error
		reloader, err = certs.New(cfg.TLSCertFile, cfg.TLSKeyFile, logger)
		if err != nil {
			return nil, nil, err
		}
	}
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return nil, nil, errors.New("cannot listen on server address")
	}
	if reloader != nil {
		return tls.NewListener(listener, &tls.Config{
			GetCertificate: reloader.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}), reloader, nil
	}
	return listener, nil, nil
}
