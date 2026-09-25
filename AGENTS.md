# AGENTS.md

## Commands

- `make fmt`: explicitly format Go source.
- `make tidy`: explicitly update module dependencies.
- `make lint`: non-mutating lint (golangci-lint 2.14.0).
- `make test`: normal and race tests, including disposable Redis integration tests.
- `./scripts/go_test.sh -run TestName ./internal/...`: focused tests. Never invoke `go test` directly; the wrapper supplies the public fixture Age identity.
- `make build`: local binary.
- `make build/all`: snapshot release binaries (GoReleaser 2.18.2).

## Architecture

The composition root loads strict configuration and constructs explicit dependencies. `internal/source` maintains a bare encrypted Git cache and reads selected commit blobs. `internal/decrypt` wraps embedded SOPS, sanitizing failures. `internal/snapshot` validates flat mappings, merges base/environment data, and publishes immutable snapshots. `internal/syncer` serializes refresh triggers and handles cache restoration, retries, freshness, and deadlines. `internal/notify` provides optional reconnecting Redis hints. `internal/httpapi` exposes role-separated read/admin routes and operational endpoints. `internal/app` owns startup and bounded shutdown.

Snapshots never expose mutable maps. Each request captures one revision. Failed or expired candidates never replace active secrets. Embedded SOPS calls can outlive cancellation; do not introduce overlapping retries or block HTTP reads on them. Persist only encrypted data and revision metadata. Never log secret values, credentials, or raw decryption errors.

## Conventions

Use structured `slog`, real propagated contexts, standard-library assertions (or cmp), and grouped imports: stdlib, third party, then local module. Sort and align struct tags. Keep `os.Exit` in main only. No mutable package globals apart from build metadata. Pin dependencies and tooling; validation targets must not rewrite files.
