package app

import (
	"crypto/tls"
	"errors"
	"net"

	"github.com/zekihan/external-secrets-sops/internal/config"
)

// listen enforces transport policy even when Run is called without config.Load.
func listen(cfg config.Server) (net.Listener, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	var tlsConfig *tls.Config
	if cfg.TLSCertFile != "" {
		certificate, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, errors.New("cannot load server TLS certificate and key")
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS12,
		}
	}
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return nil, errors.New("cannot listen on server address")
	}
	if tlsConfig != nil {
		return tls.NewListener(listener, tlsConfig), nil
	}
	return listener, nil
}
