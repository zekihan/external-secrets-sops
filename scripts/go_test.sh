#!/bin/sh
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$DIR/.."
unset SOPS_AGE_KEY SOPS_AGE_KEY_CMD
export SOPS_AGE_KEY_FILE="$PWD/testdata/key.txt"
go test "$@"
