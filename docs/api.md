# HTTP API

Send `Authorization: Bearer <token>` for authenticated routes. Reader and admin roles are disjoint; admin tokens cannot read secrets. All responses use `Cache-Control: no-store`. Secret success responses carry `X-Secret-Revision` with the Git commit hash.

| Method and path | Role | Response |
| --- | --- | --- |
| `GET /v1/secrets/{project}/{config}/{environment}` | reader | `{"data":{"KEY":"value"}}` |
| `GET /v1/secrets/{project}/{config}/{environment}/{key}` | reader | `{"value":"value"}` |
| `POST /v1/webhooks/github` | GitHub HMAC-SHA256 | signed ping/filtered push; see below |
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
Bootstrap and private records remain outside the runtime root. Service defaults use
`encrypted/runtime`. Homelab legacy examples are retired locally. Generic configurable roots remain
supported; rollback requires matched historical code and encrypted data. See the [execution record](https://github.com/zekihan/argocd/blob/main/docs/secrets-layout-execution-2026-10-27.md).

## GitHub refresh receiver

`POST /v1/webhooks/github` authenticates `X-Hub-Signature-256` (`sha256=<hex>`)
against the exact raw request body using HMAC-SHA256 and a constant-time digest
comparison. It does not require a Bearer token. Bodies are limited to 1 MiB
(413 above the limit); missing or invalid signatures receive 401. Bodies and
signatures are never logged. Configure GitHub to send JSON (`application/json`).

`auth.githubWebhookSecretEnv` names the environment variable containing the
shared secret and defaults to `ESS_GITHUB_WEBHOOK_SECRET`. An unset/empty secret
or empty environment reference disables acceptance (401); startup and other
routes continue normally. Restart the process after supplying or rotating it.

Signed `ping` events return 200 with `{"message":"pong"}`. Signed `push` events
must contain `ref`; only `refs/heads/<git.branch>` schedules refresh. Other events
and branches return 200 with `{"ignored":true}`. Malformed push JSON or a missing
ref returns 400. The receiver fetches configured Git state; payload contents never
supply secret data or select a commit to publish.

Matching pushes share the admin refresh trigger and Redis broadcast, returning
202 with `accepted` and `broadcast` (`sent`, `failed`, or `disabled`). Broadcast
failure still queues local work and returns 202. This promises eventual refresh,
not immediate fan-out or completed publication; polling remains the fallback.

Webhook refreshes are limited to one per ten seconds per process, including
concurrent requests and repeated deliveries with unchanged, changed, or missing
`X-GitHub-Delivery`. Suppressed pushes return 202 with `{"coalesced":true}` and do
not trigger or broadcast again. This bound resets on restart and is independent
per replica. A push suppressed during the interval is eventually picked up by
polling or a later refresh; the receiver does not schedule a trailing timer.
Admin refreshes and polling are unaffected.
