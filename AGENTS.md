# AGENTS.md

This file provides guidance to AI Agents when working with code in this repository.

## Commands

```sh
# Format and tidy
make tidy

# Run tests (sets required env vars automatically via scripts/go_test.sh)
make test

# Run a single test
./scripts/go_test.sh -v -run TestName ./package/...

# Lint
make lint

# Build for local platform
make build

# Run the binary after building
make run
```

Tests require `CONFIG_DIR` and `SOPS_AGE_KEY_FILE` env vars — `scripts/go_test.sh` sets these automatically (points to `testdata/key.txt`). Never call `go test` directly; always go through the script.

## Architecture

This is an HTTP server that serves SOPS-encrypted secrets from a Git repository. The flow:

1. **Git sync** (`git/`) — on startup, clones the configured repo and runs a periodic pull loop (`StartPullLoop`). A force-sync can also be triggered via the API or via a Redis pubsub event from a GitHub webhook.

2. **Secret resolution** (`secret/`) — reads YAML files at `<repoPath>/<secretsPath>/<project>/<config>/<environment>/values.yaml`, decrypts with SOPS (Age key via `SOPS_AGE_KEY_FILE`). When the environment is not `"base"`, values from `base/values.yaml` are merged first, with environment-specific values taking precedence.

3. **HTTP routes** (`routes/api/`, wired in `main.go`):
   - `GET /secret/{project}/{config}/{environment}` — all secrets for that environment
   - `GET /secret/{project}/{config}/{environment}/{key}` — single secret value
   - `GET /git/lastCommit` — last synced commit
   - `POST /git/forceSync` — trigger immediate git pull
   - `GET /api/ready` — readiness (depends on git repo being cloned)
   All secret/git routes require an API key (passed as `ESS__WEB_API_KEY`).

4. **PubSub** (`pubsub/`) — optional Redis-backed pub/sub. When Redis is enabled, GitHub events published on the `github-events` channel trigger an immediate `git.Pull`. Falls back to in-process pub/sub when Redis is disabled.

5. **Config** (`config/`) — loaded from `config.yaml` (or `CONFIG_PATH`/`CONFIG_DIR` env vars), then overridden by env vars prefixed with `ESS__`. Key prefixes: `ESS__GIT_*`, `ESS__WEB_*`, `ESS__LOG_*`, `ESS__REDIS_*`.

## Code conventions

- Use `slog` with structured attributes for all logging; `log.Print*` and `log.Fatal*` are linted away.
- `context.TODO` is forbidden — always propagate a real context.
- Imports are grouped: stdlib → third-party → local (`github.com/zekihan/external-secrets-sops`), enforced by `gci`.
- Struct tags must be sorted and aligned per `.golangci.yaml` (`tagalign`).
- `github.com/stretchr/testify` is banned — use `cmp` or standard library assertions in tests.
- `os.Exit` is forbidden outside `main.go` and config initialization.
