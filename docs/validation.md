# Rewrite validation

Validation uses public fixtures and local temporary Git/Redis servers; no production secrets or cluster changes.

- `make lint`: non-mutating lint, all configured checks.
- `make test`: full normal and race-enabled suites, including real Age/PGP decryption, rejected candidates, Redis outage/reconnection with two HTTP replicas, and bounded shutdown.
- `make build/all`: Linux, macOS, and Windows on amd64/arm64, plus universal macOS binary.
- `ESS_CHART_RENDER_DIR=/path/to/rendered-manifests ./scripts/go_test.sh -run TestRenderedChart -v ./internal/config`: optionally validate Kubernetes manifests for the `example` project in `staging` and `prod`, with Redis enabled and disabled. Render filenames are `<environment>-<true|false>.yaml`. Each variant must contain a ConfigMap, Deployment, and ClusterSecretStore; assertions cover scope, Redis credential references, probes, a 256 MiB memory limit, and the ESO API contract.

## Local performance sample

Measured on macOS arm64, Apple M5 Pro, Go 1.27.1. These are development-machine measurements, not a production capacity guarantee.

| Workload | Result |
| --- | --- |
| In-memory single-key lookup | 19.51 ns/op, zero allocations |
| Build 20 plain documents with 100 128-character values each | 1.01 ms/op, 1.98 MB allocated/op |
| Build/decrypt 20 Age-encrypted documents with the same contents, with an old snapshot present | 17.81 ms/op, 74.57 MB cumulatively allocated/op |
| Peak RSS for ten encrypted candidate builds | 53,952,512 bytes (51.5 MiB) |

Cumulative allocation includes memory reclaimed during a build; it is not resident memory. The encrypted benchmark uses four decryption workers. This sample supports an initial 256 MiB memory limit for a modest scoped dataset; benchmark again when increasing scope, limits, or document sizes.

Reproduce:

```sh
./scripts/go_test.sh -run '^$' -bench . -benchmem ./internal/snapshot
./scripts/go_test.sh -c -o tmp/snapshot.test ./internal/snapshot
SOPS_AGE_KEY_FILE="$PWD/testdata/key.txt" /usr/bin/time -l ./tmp/snapshot.test \
  -test.run '^$' -test.bench BenchmarkEncryptedCandidate -test.benchtime=10x -test.benchmem
```

SOPS policy tests use synthetic remote metadata, a loopback HTTP trap with a fixture Vault token, and public local Age/PGP keys. They cover flat/grouped backend rejection before key recovery, recipient/group/candidate budgets, ambiguous YAML, Age plugin rejection, MAC tampering, missing/wrong identities, and retaining the active snapshot after candidate rejection. Remote backends are disabled; no live cloud KMS calls are needed. Local helper cancellation, container runtime behavior, scoped production recipient compatibility, and cluster deployment remain outside this local validation. Publish/verify the new image and pin its actual digest before deployment.
