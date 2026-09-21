# Public test fixtures

`key.txt` and `.sops.yaml` contain a public, test-only Age identity/recipient. Existing generated documents and source examples contain invented values only. New integration tests generate encrypted documents in memory and commit them into temporary local Git repositories.

`pgp/private.asc` is an unprotected, test-only PGP private key for `fixture@example.invalid`. `pgp/values.yaml` is its encrypted fixture. Tests convert it into a temporary binary keyring and use embedded OpenPGP, without invoking GnuPG or reading the user's keyring.

Never use these identities for real secrets. The test wrapper sets `SOPS_AGE_KEY_FILE` automatically.
