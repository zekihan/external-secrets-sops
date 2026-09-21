#!/bin/sh
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$DIR/.."
export SOPS_AGE_KEY_FILE="$PWD/testdata/key.txt"
go test "$@"
