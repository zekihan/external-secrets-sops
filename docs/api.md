# HTTP API

Send `Authorization: Bearer <token>` for authenticated routes. Reader and admin roles are disjoint; admin tokens cannot read secrets. All responses use `Cache-Control: no-store`. Secret success responses carry `X-Secret-Revision` with the Git commit hash.

| Method and path | Role | Response |
| --- | --- | --- |
| `GET /v1/secrets/{project}/{config}/{environment}` | reader | `{"data":{"KEY":"value"}}` |
| `GET /v1/secrets/{project}/{config}/{environment}/{key}` | reader | `{"value":"value"}` |
| `POST /v1/refresh` | admin | `202 {"accepted":true,"broadcast":"sent"}` |
| `GET /v1/status` | admin | instance status, described below |
| `GET /metrics` | admin | Prometheus text exposition |
| `GET /livez` | public | `{"live":true}` |
| `GET /readyz` | public | `{"ready":true}` or 503 |

Path components must be nonempty and cannot contain separators, control characters, `.` or `..`. Percent-encoded separators are also invalid. Empty secret values are `{"value":""}`; empty environments are `{"data":{}}`.

Refresh accepts no input. Local refresh is always queued before broadcast is attempted. `broadcast` is `sent`, `failed`, or `disabled`; a failed broadcast still returns 202. This acknowledges scheduling, not completed publication. Redis consumers treat every channel message as an opaque refresh hint and never log payloads.

Status fields: `revision`, `validatedAt` (null before first snapshot), `ageSeconds`, `documents`, `bytes`, `ready`, `updating`, `phase`, `lastAttempt`, `lastError`, `successes`, `failures`, `durationSeconds`, `totalDurationSeconds`, `redisEnabled`, and `redisConnected`. Phases are `restoring`, `fetching`, `reading`, `decrypting`, `persisting`, and `idle`. Failure codes identify fetch, read, validation, cache-write, or deadline failures. Timestamps are RFC3339. Status and metrics represent the receiving instance, not the whole deployment. `bytes` counts merged key/value bytes, not process RSS.

Metrics: `ess_refresh_total{outcome}`, `ess_refresh_duration_seconds_sum`, `ess_refresh_duration_seconds_count`, `ess_http_errors_total`, `ess_redis_connected`, `ess_snapshot_age_seconds`, `ess_snapshot_bytes`, `ess_snapshot_documents`, and `ess_snapshot_info{revision}`. Snapshot series appear after the first publication.

Errors use `{"error":"code"}` with 400 invalid input, 401 missing/invalid credentials, 403 wrong role, 404 absent document/key, 405 unsupported method, 503 missing/expired snapshot, or 500 internal failure. Error bodies never contain underlying Git/SOPS errors, secret values, or credential material.

External Secrets webhook configuration should use the all-secrets route, a reader Bearer token, and `result.jsonPath: $.data`.

## Runtime-layout coordinates

For the new runtime layout, configure the root as
`encrypted/runtime`, projects as `[apps, shared]`, and environments as only the
service's own cluster. App reads then use `/v1/secrets/apps/<app>/<cluster>`.
Shared documents resolve through descriptors within that same configured scope.
Do not widen online scope to production/local to satisfy offline consumer refs;
use the offline `resolve` command with an explicit dependency scope instead.
Bootstrap and private records remain outside the runtime root. Existing service
defaults retain `generated` for backward compatibility. The completed homelab
cluster-1 deployment explicitly selects the new layout; shipped examples are
not evidence of its active root. See the [execution record](https://github.com/zekihan/argocd/blob/main/docs/secrets-layout-execution-2026-10-27.md).
