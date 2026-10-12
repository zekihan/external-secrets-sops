# external-secrets-sops

A Go HTTP provider for SOPS-encrypted secrets in Git. Each response comes from one validated, immutable Git revision. Designed for the External Secrets Operator webhook provider and standalone HTTP clients.

## Run

Requires Go 1.27.1 to build. SOPS is embedded; no SOPS executable is required. Use standard SOPS credentials for Age, PGP, or cloud KMS. GnuPG-backed keyrings and external credential helpers require those executables in a customized image; in-process PGP keyrings do not.

```sh
make build
export ESS_READER_TOKEN='replace-with-reader-token'
export ESS_ADMIN_TOKEN='replace-with-different-admin-token'
export ESS_GIT_TOKEN='token-for-private-git-repository'
export SOPS_AGE_KEY_FILE='/absolute/path/to/age/keys.txt'
./dist/external-secrets-sops check-config --config config.yaml
./dist/external-secrets-sops serve --config config.yaml
```

Edit `config.yaml` first: its local Git path is an example, not an initialized repository. `check-config` is offline and validates YAML, settings, and required API/Redis credential presence; it does not test Git authentication or decryption.

Operational settings come exclusively from strict YAML. Unknown fields and multiple YAML documents are rejected. Application credentials are named by `auth.readerTokenEnv`, `auth.adminTokenEnv`, `git.tokenEnv`, and `redis.passwordEnv`. The Git token is optional for public/local repositories; set the reference to an empty string to disable lookup. When Redis is enabled, a nonempty password reference requires the variable to be present. API tokens must be distinct, nonempty, and contain no whitespace. Restart after configuration or credential changes.

Only HTTPS repository URLs without embedded credentials, or explicit local filesystem paths, are supported. Git certificates are verified using the system trust store. No SSH transport or credential-bearing URL is accepted.

See [API](docs/api.md), [operations](docs/operations.md), and the complete defaults in [config.yaml](config.yaml).

## Secret documents

```
<git.root>/<project>/<config>/<environment>/values.yaml
```

Only configured projects/environments and their required base files are loaded. Files outside that scope are ignored. Each environment requires its own SOPS-encrypted file; an empty mapping explicitly inherits base. Base is optional. To serve base directly, include `base` in the configured environments.

Environment keys override base keys. Null removes a key; empty strings remain values. Strings, numbers, and booleans are returned as strings using the decrypted YAML scalar text. Nested values, sequences, duplicate keys, aliases, YAML merge keys, unsafe path components, symlinks, and submodules are rejected. A candidate with any invalid selected document, or no selected environment documents, never replaces the active snapshot.

Default bounds are four decrypt workers, 1 MiB per encrypted/decrypted document, and 32 MiB per candidate (encrypted input, decrypted input, and merged values each checked). These are data limits, not a bound on total process memory or SOPS parsing allocations.

## Updates and failures

One worker serializes startup, polling, manual refresh, and Redis triggers. Repeated triggers during an update coalesce into one subsequent pass. Polling defaults to five minutes with ±10% jitter; failures retry from five seconds up to five minutes. Redis reconnects independently and requests a refresh after reconnecting. Notifications carry no secret data.

Git fetches read a bare object database, never a live worktree. The configured branch is resolved even when fetch returns unchanged. Force pushes and rollbacks are supported. Publication swaps the whole scoped snapshot atomically. Multiple replicas converge independently, so clients may observe different complete revisions during propagation.

The last valid snapshot stays available indefinitely by default. Set `refresh.maxStaleness` to stop serving once remote validation is too old. An unchanged valid remote revision renews freshness; an invalid newer revision does not. On disk, only encrypted Git objects and validated revision metadata are stored. Restart recovery re-decrypts and revalidates the cached revision.

SOPS' embedded API does not consistently support cancellation. An operation may outlive the refresh timeout, delaying future refreshes; it cannot block snapshot reads or cause overlapping attempts. Expired candidates are discarded. Process shutdown is bounded by the configured grace period.

## Development

Install Redis server for integration tests, golangci-lint **2.14.0**, and GoReleaser **2.18.3** for release builds.

```sh
make fmt                 # explicit source formatting
make tidy                # explicit module updates
make lint                # read-only validation
make test                # normal and race-enabled tests
make build
make build/all           # snapshot release binaries
./scripts/go_test.sh -run '^$' -bench . -benchmem ./internal/snapshot
```

The wrapper supplies only the public fixture Age identity. Tests never require production credentials; PGP uses a dedicated checked-in test key. Integration tests start disposable Redis and HTTP listeners on loopback and use temporary local Git repositories.

For Docker Compose, create `.env` containing the two API tokens and optional Git token, set `SOPS_AGE_KEY_FILE`, review `docs/compose-config.yaml`, and first run `make build/release/docker`. Compose uses a private Redis container without a host port. The image contains one application executable and the existing pinned runtime base.

Version 2 replaces the v1 API and configuration completely. There are no compatibility aliases.
