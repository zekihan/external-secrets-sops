#!/bin/sh

set -e

DIR=$( cd -P -- "$(dirname -- "$(command -v -- "$0")")" && pwd -P )

cd "${DIR}/.."

PROJECT_DIR=$(pwd)

export CONFIG_DIR="$PROJECT_DIR"
export SOPS_AGE_KEY_FILE="$PROJECT_DIR/testdata/key.txt"

go test "$@"
