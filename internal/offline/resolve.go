// Package offline resolves local secret bundles without starting the service or
// fetching remote data. Plaintext exists only in memory and the caller's output.
package offline

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/decrypt"
	"github.com/zekihan/external-secrets-sops/internal/snapshot"
	"github.com/zekihan/external-secrets-sops/internal/source"
)

type Options struct {
	Input, Root, Repo, Revision, Bundle string
	Projects, Environments              []string
	Limits                              config.Limits
}

type synthetic struct{}

func (synthetic) Decrypt(ctx context.Context, data []byte) ([]byte, error) {
	return data, ctx.Err()
}

func Resolve(ctx context.Context, opts Options) (map[string]string, error) {
	cfg := config.Defaults()
	cfg.Git.URL = "./offline"
	cfg.Git.Root = "."
	cfg.Scope = config.Scope{Projects: opts.Projects, Environments: opts.Environments}
	cfg.Limits = opts.Limits
	if opts.Root == "" {
		return nil, errors.New("root is required")
	}
	switch opts.Input {
	case "dummy":
		if opts.Repo != "" || opts.Revision != "" {
			return nil, errors.New("dummy input does not accept repo or revision")
		}
	case "sops":
		if opts.Repo == "" || opts.Revision == "" {
			return nil, errors.New("sops input requires repo and revision")
		}
		cfg.Git.Root = opts.Root
	default:
		return nil, errors.New("input must be dummy or sops")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	parts := strings.Split(opts.Bundle, "/")
	if len(parts) != 3 {
		return nil, errors.New("bundle must be project/config/environment")
	}
	for _, part := range parts {
		if !config.Component(part) {
			return nil, errors.New("invalid bundle component")
		}
	}
	if !slices.Contains(opts.Projects, parts[0]) || !slices.Contains(opts.Environments, parts[2]) {
		return nil, errors.New("bundle is outside selected scope")
	}
	var docs []source.Document
	var d snapshot.Decrypter
	var err error
	if opts.Input == "dummy" {
		docs, err = source.DummyDocuments(ctx, cfg, opts.Root)
		d = synthetic{}
	} else {
		docs, err = source.LocalDocuments(ctx, cfg, opts.Repo, opts.Revision)
		d = decrypt.SOPS{}
	}
	if err != nil {
		return nil, err
	}
	candidate, err := snapshot.NewBuilder(cfg, d).Build(ctx, opts.Revision, time.Now(), docs)
	if err != nil {
		return nil, err
	}
	values, ok := candidate.Values(
		snapshot.Key{Project: parts[0], Config: parts[1], Environment: parts[2]},
	)
	if !ok {
		return nil, errors.New("selected bundle unavailable")
	}
	return values, nil
}
