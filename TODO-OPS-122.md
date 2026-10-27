# OPS-122 validation and remaining checks

Based on main `762358d5b897ffef59f977d339003c6ba4eb9e36`, which already restored
the GHCR base pin. This change adds the missing final-container regression gate.

Completed locally on 2026-10-27:

- `make lint` (golangci-lint 2.15.0): zero issues.
- `make test`: full normal and race suites, including disposable Redis integration.
- `make build/release/docker` (GoReleaser 2.19.0): Linux amd64 and arm64 binaries.
- `make test/container`: anonymous base resolution, final builds for both targets,
  real entrypoint/version execution offline and read-only, configured user and
  effective runtime UID/GID 25000:25000. Both architectures passed.
- `make build/all`: complete snapshot binary matrix, including macOS universal.
- `go mod verify`: module checksums verified.
- Shell syntax, ShellCheck, and `git diff --check` passed.

Remaining:

- [ ] Observe the new gate on a GitHub-hosted PR run when Actions budget permits.
  Local execution passed; hosted execution is not claimed. No workflow dispatch,
  retry, billing change, or check weakening was performed.

No publication or deployment is needed for these local acceptance checks. Release
publication and any deployment remain outside this PR's authorization.
