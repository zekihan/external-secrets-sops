# Operations and release

## Lifecycle and cache

Startup validates configuration before listening. HTTP starts while cache restore and Git synchronization run in the background. `/livez` stays healthy through Git, SOPS, and Redis failures; `/readyz` is healthy only with a usable snapshot. Use liveness for startup probes so remote outages do not restart otherwise functional processes.

The cache directory must be private and writable by the process. Each repository URL/branch pair receives its own directory. A fingerprint prevents restoring a snapshot with incompatible scope, path, or limits. Corrupt Git caches are moved aside under a timestamped `.invalid-` name before fetching afresh; periodically remove these diagnostic directories during maintenance. No plaintext cache exists.

A Kubernetes deployment can use `emptyDir` for the encrypted cache; it survives a container restart but not replacement of the pod. Restoring a cached encrypted revision still needs local decryption credentials and must pass the current metadata policy. Redis is optional and cannot prevent initial secret availability. During bootstrap disable Redis until its own credentials and service exist.

A refresh deadline prevents late publication, but cannot forcibly stop embedded SOPS backend calls. Monitor `updating`, `lastAttempt`, and snapshot age to detect stuck operations. On termination the service stops new work, drains HTTP, and waits at most `server.shutdownTimeout`; Kubernetes termination grace should exceed that by ten seconds.

## API transport

> **BREAKING CHANGE — native TLS is mandatory for non-loopback API listeners.** Existing wildcard, hostname and other non-loopback HTTP deployments will fail startup until both TLS files are configured. TLS termination at an ingress alone does not satisfy this requirement. Merging the source does not migrate deployments, provision certificates or validate their trust paths.

Before any separately approved rollout:

1. **Mount certificate and key files.** Supply the existing PEM certificate chain and matching private key using read-only mounts readable by the application UID. Set `server.tlsCertFile` and `server.tlsKeyFile` to those paths; Compose requires `ESS_TLS_CERT_FILE` and `ESS_TLS_KEY_FILE`. A loopback host port does not exempt a wildcard container listener. Confirm the certificate SANs match the names used by clients and proxies.
2. **Migrate ESO consumers and probes to HTTPS.** Update the External Secrets Operator webhook URL and every direct client, startup, liveness, readiness and load-balancer health check that reaches the TLS listener. Configure ESO/client CA trust explicitly as needed and verify authenticated secret reads and reader/admin separation. [Kubernetes HTTPS probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) skip server certificate verification, so their success does not establish that ESO or Traefik trusts the certificate; validate those client paths separately. Keep liveness on `/livez` and readiness on `/readyz`.
3. **Validate Traefik's upstream TLS.** Configure the backend service to use HTTPS to this application's port, not just HTTPS on Traefik's public router. Attach an appropriate ServersTransport with the approved CA trust and server name/SNI matching the backend certificate. Keep `insecureSkipVerify` false; do not bypass chain or hostname validation. Confirm a valid upstream succeeds and wrong-name/untrusted-certificate upstreams fail in a separately approved environment. See [Traefik ServersTransport](https://doc.traefik.io/traefik/reference/routing-configuration/http/load-balancing/serverstransport/) for provider-specific configuration. No live Traefik path has been validated by these local tests.
4. **Restart after certificate renewal.** Certificates are loaded once before listening; replacing mounted files does not reload them. Arrange an operator-controlled restart after renewal and verify the served certificate, readiness, ESO reads and proxy connectivity. Account for projected-volume/file-mount update behavior and refresh any mount that still references the old file before restarting.
5. **Bootstrap independently of this provider.** The TLS certificate/key, required CA trust, API tokens and local Age/PGP decryption credentials must already be available without an ExternalSecret that calls this same service. Their renewal/recovery path must also remain available while the service is down. Keep bootstrap Redis disabled until its own prerequisites exist, and ensure scoped Git files and cached revisions meet the current local-recipient policy. Do not introduce a self-dependent certificate or credential fetch to make the API ready.

A source merge is not approval to change GitOps, publish/deploy an image, issue credentials, or modify trust settings. Live migration and acceptance remain separate operator work.

HTTP listeners must use a literal loopback IP; hostnames (including `localhost`) are rejected for cleartext to avoid depending on DNS resolution. An empty host, `0.0.0.0`, `[::]`, and all other non-loopback addresses require native TLS:

```yaml
server:
  address: 0.0.0.0:8080
  tlsCertFile: /credentials/tls.crt
  tlsKeyFile: /credentials/tls.key
```

Supply an existing PEM certificate chain and matching PEM private key through read-only mounts accessible to the process. Both fields are required together, even on loopback. Invalid or missing files fail startup before accepting connections. The minimum TLS version is 1.2; restart to reload renewed certificates. `check-config` checks transport settings but does not open certificate files; `serve` verifies them before listening.

This changes existing wildcard/hostname plaintext deployments: configure TLS and update clients/probes to HTTPS before rollout. A host-loopback published Docker port does not make the container's wildcard listener loopback; the supplied Compose configuration now requires `ESS_TLS_CERT_FILE` and `ESS_TLS_KEY_FILE` mounts. Remote proxies must connect to the backend over TLS and verify its identity, or share its loopback network namespace. There is no trusted TLS-terminator plaintext exception. Authorization tokens, role separation, snapshot integrity, readiness and shutdown behavior remain required over either transport.

Live acceptance requires separate deployment approval: confirm the certificate name and trust chain with each consumer, mount existing certificates, update HTTPS probes, verify authenticated reads and admin role separation, and check certificate renewal/restart procedures. No live deployment or trust-store changes are performed by local tests.

## Credentials and SOPS metadata policy

For Age, mount the identity read-only and set `SOPS_AGE_KEY_FILE`. Native X25519, hybrid, and SSH recipients are supported. Keep credential files, helper programs, and their configuration under operator control, separate from the selected Git branch.

PGP can use mounted binary `secring.gpg`/`pubring.gpg` files under `GNUPGHOME` with SOPS' embedded OpenPGP implementation. Modern GnuPG keyrings and credential command helpers require the corresponding programs in a derived image. The stock image does not install them. Trusted local helpers and agents can still outlive cancellation; metadata limits do not provide a hard process execution deadline.

Remote recipients (`kms`, `gcp_kms`, `hckms`, `azure_kv`, `hc_vault`) and Age plugin recipients are disabled. The allowed remote-origin set is empty: Git metadata cannot choose a credential-bearing key-service destination. SOPS performs key recovery before checking the document MAC, so an authentic-looking envelope is insufficient to authorize backend access. Empty legacy backend arrays remain compatible. Fixed limits allow at most four groups and sixteen total recipients per document, and 256 recipients across all selected documents. Preflight checks the complete candidate before workers start; direct decrypt calls also enforce the document policy. SOPS still performs its normal decryption and MAC verification afterward.

This is a compatibility change for installations using remote backends or Age plugin recipients. Before rollout, confirm all scoped files and any cached last-known-good revision use supported local recipients within these budgets. A rejected new revision preserves an existing in-memory snapshot, but an unsupported cached revision cannot restore readiness after restart. Source inspection and local fixture tests do not establish the deployment's trust model or production exposure. Any migration of recipients, credentials, workload identity, or egress policy requires a separately reviewed deployment change. If remote support is needed later, require an operator-controlled exact-origin allowlist and enforceable provider deadlines before enabling it; restrict runtime egress independently.

## Coordinated v2 rollout

Upgrade the application and webhook consumers together; v1 consumers cannot call v2 routes. Prepare distinct reader/admin tokens and a Git token for private repositories in your credential store before rollout. Publish and verify the v2 image, pin its actual digest in your deployment, then apply the deployment and webhook changes together during a maintenance window.

Expect a temporary provider interruption while old pods drain and v2 snapshots become ready. Verify both `/readyz` and an authenticated webhook lookup, inspect revision/freshness on each replica, and confirm ExternalSecrets reconcile. Redis-enabled and Redis-disabled installations must both pass these checks.

Rollback reverts the image and deployment/SecretStore contracts together. Keep the old credentials available through the rollback window; v1 uses different credential names and routes. Do not roll back only the image. Source secret document changes are a separate operation and are not part of this rewrite.

Local build/test commands do not publish images, provision credentials, push Git changes, or modify deployments.
