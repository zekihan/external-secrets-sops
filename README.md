# external-secrets-sops

A Go HTTP provider for SOPS-encrypted secrets in Git. Each response comes from one validated, immutable Git revision. Designed for the External Secrets Operator webhook provider and standalone HTTP clients.

> **BREAKING CHANGE: networked API listeners now require native TLS.** Wildcard, hostname and other non-loopback HTTP listeners fail startup until `server.tlsCertFile` and `server.tlsKeyFile` point to mounted certificate/key files. Ingress TLS termination alone is insufficient. Before deployment, migrate ESO webhook consumers and probes to HTTPS, validate Traefik's HTTPS upstream certificate chain and server name, arrange a restart after certificate renewal, and bootstrap certificates, trust and credentials independently of this provider. Source merge does not perform or authorize that migration. See the [required transport rollout steps](docs/operations.md#api-transport).

## Run

Requires the Go version pinned in `go.mod` to build. SOPS is embedded; no SOPS executable is required. Use standard SOPS credentials for local Age or PGP keys. GnuPG-backed keyrings and external credential helpers require those executables in a customized image; in-process PGP keyrings do not. Remote KMS/Vault backends and Age plugin recipients are rejected before credential discovery or key recovery.

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

Cleartext HTTP is allowed only on literal loopback IP listeners (for example `127.0.0.1:8080` or `[::1]:8080`). Wildcard, non-loopback, and hostname listeners require both `server.tlsCertFile` and `server.tlsKeyFile`; TLS uses version 1.2 or later. Certificate/key loading must succeed before listening. There is no plaintext trusted-proxy bypass. See [transport configuration](docs/operations.md#api-transport) before upgrading networked deployments.

Only HTTPS repository URLs without embedded credentials, or explicit local filesystem paths, are supported. Git certificates are verified using the system trust store. No SSH transport or credential-bearing URL is accepted.

See [API](docs/api.md), [operations](docs/operations.md), and the complete defaults in [config.yaml](config.yaml).

## Secret documents

```
<git.root>/<project>/<config>/<environment>/values.yaml
```

Only configured projects/environments and their required base files are loaded. Files outside that scope are ignored. Each environment requires its own SOPS-encrypted file; an empty mapping explicitly inherits base. Base is optional. To serve base directly, include `base` in the configured environments.

Environment keys override base keys. Null removes a key; empty strings remain values. Strings, numbers, and booleans are returned as strings using the decrypted YAML scalar text. Except for the explicit template descriptors below, nested values, sequences, duplicate keys, aliases, YAML merge keys, unsafe path components, symlinks, and submodules are rejected. A candidate with any invalid selected document, or no selected environment documents, never replaces the active snapshot.

Default bounds are four decrypt workers, 1 MiB per encrypted/decrypted document, and 32 MiB per candidate (encrypted input, decrypted input, and merged values each checked). These are data limits, not a bound on total process memory or SOPS parsing allocations.

SOPS metadata has fixed work limits: four key groups and sixteen total recipients per document, and 256 total recipients across the candidate. All selected documents pass metadata validation before any key recovery starts. Both legacy flat recipient lists and `key_groups` are supported; every fallback recipient counts. Only local Age (X25519, hybrid, SSH) and PGP recipients are accepted. Encrypted YAML must be a single mapping without aliases, anchors, duplicate keys, or merges, with nesting limited to 32 levels. A rejected candidate leaves the active snapshot intact. These limits bound recipient dispatch, not the duration of operator-configured local helpers.

Git selection also has fixed per-candidate safety ceilings: 1 MiB and 16,384 entries per decoded tree; 16 MiB, 65,536 entries, and 16,384 tree loads across the traversal; 4,096 selected component directories and 4,096 documents. Repeated tree references consume the budget again, including directories with no `values.yaml`. Each lookup charges the tree entry count plus one against a 33,554,432-unit work budget, so unsorted trees and missing paths remain bounded. Tree entry headers must fit in 4 KiB. Trees are size-checked before materialization and entry-counted before decoding; exceeding a ceiling rejects the entire candidate before decryption/publication. These bounds apply to local selection after fetching, and do not bound Git transfer, pack/delta processing, or total process memory.

## Secret templates

Use exact single-field descriptor mappings with string values in the plaintext YAML before encrypting it with SOPS:

```yaml
USER: app
PASSWORD:
  $ref: shared/database/PASSWORD
DATABASE_URL:
  $template: 'postgres://${USER | urluserinfo}:${PASSWORD | urluserinfo}@${HOST}/${DATABASE | urlpath}'
STAGING_TOKEN:
  $ref: shared/service/staging/TOKEN
```

Ordinary strings are always literal, including strings containing `${...}`. Custom YAML tags such as `!ref` are not supported because SOPS does not preserve them. Other nested mappings and descriptor fields are rejected.

References have exactly one, three, or four slash-separated components:

- `key`: current project/config/environment.
- `project/config/key`: another document in the current environment.
- `project/config/environment/key`: an explicitly named environment.

Only documents in the configured scope at the same Git revision can be referenced. References never fetch additional paths. Direct references to `base` require `base` in `scope.environments`; implicitly loaded base values are available only through inheritance. Environment documents must still exist, even when empty.

Base/environment merging and null deletion happen before resolution. Templates inherited into an environment use that environment's overrides. A descriptor replaces the entire inherited value. References may target other derived values; cycles and missing or deleted targets reject the candidate. Substituted values are never interpreted as template source.

Reference components use percent-encoding for characters outside ASCII letters, digits, `-._~+`. Encode `%` as `%25`, spaces as `%20`, `|` as `%7C`, and braces as `%7B`/`%7D`; encode Unicode as UTF-8 bytes. Components are split before decoding and decoded exactly once. `+` remains a literal plus. Malformed escapes, invalid UTF-8, and decoded invalid path components are rejected. Within a template, `$${` emits literal `${`. Whitespace around an interpolation's reference or encoder is ignored; spaces in names must be encoded.

Each interpolation accepts at most one optional encoder:

| Encoder | Output |
| --- | --- |
| `urlquery` | URL query component; spaces become `+` |
| `urlpath` | URL path segment |
| `urluserinfo` | URL username/password component |
| `json` | JSON string including surrounding quotes |
| `base64` | Standard padded Base64 |

No implicit encoding, loops, conditionals, encoder pipelines, external lookups, or fallbacks are supported.

| Limit setting | Default | Maximum |
| --- | ---: | ---: |
| `limits.templateDepth` | 32 reference edges | 128 |
| `limits.templateValueBytes` | 1,048,576 bytes per resolved value | 16,777,216 |
| `limits.templateEvaluations` | 100,000 references per build | 1,000,000 |

All settings must be positive; zero does not disable a guard. Every reference in each resolved descriptor counts, including repeated references and cached targets. Each value is resolved once; cached dependency heights still count toward depth. Value limits apply to literals and derived outputs. Encoded output is checked incrementally, and `limits.snapshotBytes` also bounds aggregate keys and resolved values, including values being assembled.

Any resolution failure rejects the entire candidate and preserves the previous snapshot subject to existing freshness rules. No prior snapshot means the service remains unready. Diagnostics identify the failure location without printing secret values or template bodies; long location components are truncated and dependency chains do not multiply error output. Resolved plaintext is never persisted.

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

For Docker Compose, create `.env` containing the two API tokens and optional Git token, set `SOPS_AGE_KEY_FILE`, `ESS_TLS_CERT_FILE`, and `ESS_TLS_KEY_FILE` to existing credential/certificate paths, review `docs/compose-config.yaml`, and first run `make build/release/docker`. The API uses HTTPS even with a loopback host port because its container listener is non-loopback. Clients must trust the server certificate and use its matching DNS name or IP. Compose uses a private Redis container without a host port. The image contains one application executable and the existing pinned runtime base.

Version 2 replaces the v1 API and configuration completely. There are no compatibility aliases.

### Bounded Git cache

Each refresh requests only the configured branch tip at depth one into a fresh
bare candidate. Servers must support shallow fetches. Fixed ceilings apply to the
entire fetched tree, including unselected files: 64 MiB of pack transfer, 16,384
objects, 16 MiB per inflated object or delta result, and 128 MiB combined inflated
base objects, delta instructions, and declared delta results. HTTPS reference
advertisements are limited to 1 MiB; protocol response bytes are also bounded.
Pack checksums and object decoding remain mandatory. Size failures stop refresh
before decryption and leave the validated marker and active snapshot unchanged.
These ceilings may reject repositories that previously worked, even when their
selected documents fit the configured document limits.

Two fixed cache slots retain a validated generation and one candidate/previous
generation. The marker switches atomically only after normal document validation;
failed fetches remove their candidate. The next refresh replaces the inactive slot,
so divergent tips and rejected candidates cannot accumulate packs or history.
Each slot's file bytes are capped at 208 MiB including the temporary transfer;
filesystem allocation and directory metadata add platform-dependent overhead.
Use a dedicated cache directory per process/replica; concurrent writers sharing a
cache directory are unsupported. A volume quota remains useful for metadata,
filesystem failures, and other applications sharing the volume.

Upgrades can restore an existing legacy bare cache offline. Successful refreshes
use the new slots; legacy objects and previously quarantined `.invalid-*`
directories are never extended but remain on disk. Operators should review and
remove those legacy files separately after confirming recovery from a new slot.
No automatic deletion of legacy diagnostic data occurs. Rollout sizing, legacy
cleanup, and live repository authentication/availability acceptance require
operator review; no trust, credential, or network policy changes are needed.

### Final container validation

Run `make build/release/docker` followed by `make test/container` with a local
Unix-socket Docker engine supporting Linux amd64 and arm64 execution (Docker
Desktop, or an engine with QEMU/binfmt configured). The PR workflow runs the same
container check after building binaries. It enforces digest pins, pulls the bases
with an empty Docker credential configuration, builds and loads each architecture,
runs the real `version` entrypoint offline with a read-only filesystem, and checks
both the image's configured user and effective UID/GID 25000 using a static probe.
Temporary image tags and probe files are removed afterward; nothing is pushed.
The probe is mounted only for the check and is not included in release images.

The runtime mirror is `ghcr.io/zekihan/distroless:v1.0.0`, pinned to the OCI index
`sha256:f2ec17ff44138983a5e892ab05a4eb91ea58e33d3bb263830713823ef13531c6`.
Its Linux child manifests are amd64
`sha256:a0df44f8431b78ceabafb9dbe8cda36895be93e99ec579ca36aa9435aadadc61`
and arm64
`sha256:4d3cc2e945625d61ae3180cc290a81d76e2183532923637c98dcae89385b0178`.
On 2026-10-27 the prior Docker Hub digest
`sha256:c5df9133291ff067b2a0a9ce2624fbf54aa4e6d72d0fb1a3a9ada39754a83a98`
returned `not found`; this establishes failure to resolve that reference, not
proof that it was deleted. Registry publication destinations are unchanged.
