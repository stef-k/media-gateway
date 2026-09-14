#!/usr/bin/env bash
# Package only tracked reusable artifacts from a clean revision; never operator files.
set -euo pipefail
if [[ $# != 1 ]]; then
    echo 'Usage: scripts/build-bundle.sh OUTPUT_DIRECTORY (outside checkout)' >&2
    exit 2
fi
cd "$(dirname "$0")/.."
root=$(git rev-parse --show-toplevel)
out=$(realpath -m -- "$1")
case "$out/" in "$root/"*) echo 'Output must be outside checkout' >&2; exit 2;; esac
[[ -z $(git status --porcelain --untracked-files=all) ]] || {
    echo 'A clean checkout is required' >&2; exit 1;
}
revision=$(git rev-parse HEAD)
mkdir -p -- "$out"
archive="$out/media-gateway-linux-amd64-$revision.tar.gz"
[[ ! -e "$archive" ]] || { echo 'Archive already exists' >&2; exit 1; }
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT
# Explicit flags retain normal VCS metadata and avoid ambient build flag injection.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 GOFLAGS= go build \
    -buildvcs=true -o "$stage/media-gateway" ./cmd/media-gateway
install -m 0644 deploy/config.toml.example deploy/media-gateway.service \
    deploy/nginx.conf LICENSE "$stage/"
install -m 0644 docs/release.md "$stage/README.md"
install -m 0755 scripts/smoke-deployment.py "$stage/"
# Include referenced operator authorities for offline use.
mkdir "$stage/docs"
install -m 0644 docs/{deployment,configuration,logging,toolchain,consumer-api}.md "$stage/docs/"
(
    cd "$stage"
    find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS
    sha256sum --check --strict SHA256SUMS
    # Stable archive metadata/order; this is not a byte-reproducible-build claim.
    tar --sort=name --mtime="@$(git -C "$root" show -s --format=%ct HEAD)" \
        --owner=0 --group=0 --numeric-owner -cf - . | gzip -n > "$archive"
)
echo "$archive"
