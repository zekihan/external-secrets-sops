# Operations and release

## Lifecycle and cache

Startup validates configuration before listening. HTTP starts while cache restore and Git synchronization run in the background. `/livez` stays healthy through Git, SOPS, and Redis failures; `/readyz` is healthy only with a usable snapshot. Use liveness for startup probes so remote outages do not restart otherwise functional processes.

The cache directory must be private and writable by the process. Each repository URL/branch pair receives its own directory. A fingerprint prevents restoring a snapshot with incompatible scope, path, or limits. Corrupt Git caches are moved aside under a timestamped `.invalid-` name before fetching afresh; periodically remove these diagnostic directories during maintenance. No plaintext cache exists.

A Kubernetes deployment can use `emptyDir` for the encrypted cache; it survives a container restart but not replacement of the pod. Restoring a cached encrypted revision still needs decryption credentials and, for cloud KMS, the relevant backend. Redis is optional and cannot prevent initial secret availability. During bootstrap disable Redis until its own credentials and service exist.

A refresh deadline prevents late publication, but cannot forcibly stop embedded SOPS backend calls. Monitor `updating`, `lastAttempt`, and snapshot age to detect stuck operations. On termination the service stops new work, drains HTTP, and waits at most `server.shutdownTimeout`; Kubernetes termination grace should exceed that by ten seconds.

## Credentials and cloud providers

For Age, mount the identity read-only and set `SOPS_AGE_KEY_FILE`. Other SOPS backends use their standard environment and credential discovery. Configure provider environment variables and read-only credential mounts in your deployment. Grant only the required identity permissions and add specific egress rules for private KMS endpoints. Configure workload identity token projection and cloud-provider identity explicitly when required.

PGP can use mounted binary `secring.gpg`/`pubring.gpg` files under `GNUPGHOME` with SOPS' embedded OpenPGP implementation. Modern GnuPG keyrings, credential command helpers, and Age plugins require the corresponding programs in a derived image. The stock image does not install them.

## Coordinated v2 rollout

Upgrade the application and webhook consumers together; v1 consumers cannot call v2 routes. Prepare distinct reader/admin tokens and a Git token for private repositories in your credential store before rollout. Publish and verify the v2 image, pin its actual digest in your deployment, then apply the deployment and webhook changes together during a maintenance window.

Expect a temporary provider interruption while old pods drain and v2 snapshots become ready. Verify both `/readyz` and an authenticated webhook lookup, inspect revision/freshness on each replica, and confirm ExternalSecrets reconcile. Redis-enabled and Redis-disabled installations must both pass these checks.

Rollback reverts the image and deployment/SecretStore contracts together. Keep the old credentials available through the rollback window; v1 uses different credential names and routes. Do not roll back only the image. Source secret document changes are a separate operation and are not part of this rewrite.

Local build/test commands do not publish images, provision credentials, push Git changes, or modify deployments.
