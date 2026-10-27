# Workflow security (OPS-43)

External Actions are pinned to full upstream commit SHAs. The adjacent version
comments are review aids, not execution selectors. Renovate's GitHub Actions
manager must retain digest pins; do not replace a hash with a version tag.

Run the offline policy and regression suite before merging workflow changes:

```sh
python3 -m venv /tmp/workflow-policy-venv
/tmp/workflow-policy-venv/bin/pip install -r .github/scripts/requirements-policy.txt
/tmp/workflow-policy-venv/bin/python .github/scripts/check_action_pins.py
/tmp/workflow-policy-venv/bin/python -m unittest discover -s tests -v
```

The Dockerfiles regression suite additionally requires mikefarah/yq and Git.
The policy parses YAML, including quoted keys, aliases and flow mappings. It
rejects mutable Actions, persisted checkout credentials, workflow-level write
permissions, and workflow/job-level secret environment variables. Positive and
negative fixtures run without registry credentials or real publication.

## Pin review record

Verified on 2026-10-27 directly against each official upstream repository using
`git ls-remote` for the named release tag and its peeled `^{}` commit. This
establishes the release-to-commit identity; it is not a full audit of the bundled
Action code or proof of upstream publisher security. Review changes to Action
inputs, runtime and bundled source when updating these pins.

| Action release | Immutable commit |
| --- | --- |
| [actions/checkout@v7.0.1](https://github.com/actions/checkout/tree/v7.0.1) | [3d3c42e5aac5ba805825da76410c181273ba90b1](https://github.com/actions/checkout/commit/3d3c42e5aac5ba805825da76410c181273ba90b1) |
| [actions/setup-go@v7.0.0](https://github.com/actions/setup-go/tree/v7.0.0) | [b7ad1dad31e06c5925ef5d2fc7ad053ef454303e](https://github.com/actions/setup-go/commit/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e) |
| [docker/build-push-action@v7.4.0](https://github.com/docker/build-push-action/tree/v7.4.0) | [c3c9e263c25d99ce0380d002d59b67737d91b0dc](https://github.com/docker/build-push-action/commit/c3c9e263c25d99ce0380d002d59b67737d91b0dc) |
| [docker/login-action@v4.6.0](https://github.com/docker/login-action/tree/v4.6.0) | [dbcb813823bdd20940b903addbd779551569679f](https://github.com/docker/login-action/commit/dbcb813823bdd20940b903addbd779551569679f) |
| [docker/metadata-action@v6.2.0](https://github.com/docker/metadata-action/tree/v6.2.0) | [dc802804100637a589fabce1cb79ff13a1411302](https://github.com/docker/metadata-action/commit/dc802804100637a589fabce1cb79ff13a1411302) |
| [docker/setup-buildx-action@v4.4.1](https://github.com/docker/setup-buildx-action/tree/v4.4.1) | [f87e5991a6d7451dcb8d9637bfbc97413f497069](https://github.com/docker/setup-buildx-action/commit/f87e5991a6d7451dcb8d9637bfbc97413f497069) |
| [docker/setup-qemu-action@v4.4.0](https://github.com/docker/setup-qemu-action/tree/v4.4.0) | [99012661954931238ded8c8b007157a8430204e1](https://github.com/docker/setup-qemu-action/commit/99012661954931238ded8c8b007157a8430204e1) |
| [golangci/golangci-lint-action@v9.3.0](https://github.com/golangci/golangci-lint-action/tree/v9.3.0) | [ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a](https://github.com/golangci/golangci-lint-action/commit/ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a) |
| [goreleaser/goreleaser-action@v7.3.0](https://github.com/goreleaser/goreleaser-action/tree/v7.3.0) | [a4f02ab5355a589de60d917a659ce617ed19b3de](https://github.com/goreleaser/goreleaser-action/commit/a4f02ab5355a589de60d917a659ce617ed19b3de) |
| [peter-evans/dockerhub-description@v5.0.0](https://github.com/peter-evans/dockerhub-description/tree/v5.0.0) | [1b9a80c056b620d92cedb9d9b5a223409c68ddfa](https://github.com/peter-evans/dockerhub-description/commit/1b9a80c056b620d92cedb9d9b5a223409c68ddfa) |

## Credential scope

Checkout never persists credentials. Docker Hub description credentials exist
only in the pinned description Action inputs; the redundant registry login and
workflow-wide credential environment were removed. Image publishing credentials
are inputs to pinned login steps, and release tokens remain step-scoped. Write
permissions are declared only on the jobs that require them. Image/description
publication is restricted to main-branch push or explicit main dispatch.

The existing SOPS build/release commands and tests remain unchanged. These
workflows still build and publish within their existing job; separating GoReleaser
artifacts into another runner would be an additional hardening change.

## Docker Hub description input wiring

The pinned description Action requires nonempty `username` and `password` inputs.
Its [input helper](https://github.com/peter-evans/dockerhub-description/blob/1b9a80c056b620d92cedb9d9b5a223409c68ddfa/src/input-helper.ts)
also accepts environment fallbacks. Before credential scoping, workflow-level
`DOCKERHUB_USERNAME` and `DOCKERHUB_PASSWORD` aliases pointed at the existing
`DOCKER_USERNAME` and `DOCKER_PASSWORD` secrets. Removing that environment exposed
stale `DOCKERHUB_*` secret references in the Action inputs.

Pass the existing `DOCKER_USERNAME` and `DOCKER_PASSWORD` secrets directly to the
pinned Action's inputs and derive the repository namespace from the same username.
Do not restore workflow-wide credential exposure or provision alias secrets.
Offline contract tests resolve inputs using synthetic values for only the existing
secret names; they reject the old aliases and missing/empty required inputs, and
verify credentials remain confined to the description step. These tests perform
no authentication or publication and never read real secret values.
