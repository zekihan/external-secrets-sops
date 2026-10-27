#!/bin/sh
# Build and run both final images anonymously, without publishing anything.
set -eu
cd "$(dirname "$0")/.."
# Fail closed if either Dockerfile base loses its immutable digest pin.
awk 'toupper($1) == "FROM" { for (i=2; i<=NF; i++) if ($i !~ /^--/) { print $i; break } }' Dockerfile |
while IFS= read -r base; do
    printf '%s\n' "$base" | grep -Eq '@sha256:[[:xdigit:]]{64}$' || {
        echo "Base must be digest-pinned: $base" >&2; exit 1;
    }
done
endpoint=${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}')}
case "$endpoint" in unix://*) ;; *) echo 'Use a local Unix-socket Docker engine.' >&2; exit 1 ;; esac
buildx=$(docker info --format '{{range .ClientInfo.Plugins}}{{if eq .Name "buildx"}}{{.Path}}{{end}}{{end}}')
test -x "$buildx"
scratch=$(mktemp -d)
prefix="external-secrets-sops-check-$$"
cleanup() {
    for arch in amd64 arm64; do
        docker --host "$endpoint" image rm "$prefix:$arch" >/dev/null 2>&1 || true
    done
    rm -rf "$scratch"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
# An empty client configuration ensures public pulls cannot rely on registry logins.
# Retain only the plugin executable, including Docker Desktop user installations.
mkdir -p "$scratch/cli-plugins"
ln -s "$buildx" "$scratch/cli-plugins/docker-buildx"
export DOCKER_CONFIG="$scratch"
export DOCKER_HOST="$endpoint"
unset DOCKER_CONTEXT DOCKER_AUTH_CONFIG
for arch in amd64 arm64; do
    test -s "dist/external-secrets-sops_linux_$arch/external-secrets-sops"
    docker build --pull --platform "linux/$arch" --load -t "$prefix:$arch" .
    test "$(docker image inspect --format '{{.Os}}/{{.Architecture}} {{.Config.User}}' "$prefix:$arch")" = "linux/$arch 25000:25000"
    # Exercise the real entrypoint with its configured user and no network access.
    docker run --rm --platform "linux/$arch" --network none --read-only "$prefix:$arch" version
    # Check effective IDs inside the same image, without assuming a shell exists.
    mkdir -p "$scratch/$arch"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "$scratch/$arch/ids" ./scripts/container_ids/main.go
    docker run --rm --platform "linux/$arch" --network none --read-only \
        --mount "type=bind,src=$scratch/$arch,dst=/probe,readonly" \
        --entrypoint /probe/ids "$prefix:$arch"
done
