#!/bin/sh
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$DIR/.."
unset SOPS_AGE_KEY SOPS_AGE_KEY_CMD SOPS_AGE_SSH_PRIVATE_KEY_FILE SOPS_AGE_SSH_PRIVATE_KEY_CMD
export SOPS_AGE_KEY_FILE="$PWD/testdata/key.txt"

ESS_TEST_FIXTURE_HOME=$(mktemp -d)
export ESS_TEST_FIXTURE_HOME
trap 'rmdir -- "$ESS_TEST_FIXTURE_HOME"' EXIT

# SOPS also searches home and config directories. Isolate only the test process
# so Go can still use the developer's toolchain configuration and caches.
test_exec=$(cat <<'EOF'
sh -c 'exec env \
    HOME="$ESS_TEST_FIXTURE_HOME" \
    XDG_CONFIG_HOME="$ESS_TEST_FIXTURE_HOME" \
    USERPROFILE="$ESS_TEST_FIXTURE_HOME" \
    APPDATA="$ESS_TEST_FIXTURE_HOME" \
    GNUPGHOME="$ESS_TEST_FIXTURE_HOME" \
    "$@"' sh
EOF
)
go test -exec "$test_exec" "$@"
