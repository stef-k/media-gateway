#!/usr/bin/env bash
# Package only tracked reusable artifacts from a clean revision; never operator files.
set -euo pipefail
if [[ $# != 1 && $# != 3 ]] || [[ $# == 3 && $2 != --release ]]; then
    echo 'Usage: scripts/build-bundle.sh OUTPUT_DIRECTORY [--release vMAJOR.MINOR.PATCH] (outside checkout)' >&2
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
release_tag=${3:-}
if [[ -n "$release_tag" ]]; then
    # The tag is the sole version authority; Go reads it directly from Git.
    python3 scripts/release.py validate "$release_tag" >/dev/null
fi
mkdir -p -- "$out"
archive="$out/media-gateway-linux-amd64-$revision.tar.gz"
if [[ -n "$release_tag" ]]; then
    archive="$out/media-gateway-$release_tag-linux-amd64.tar.gz"
fi
[[ ! -e "$archive" ]] || { echo 'Archive already exists' >&2; exit 1; }
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"; rm -f -- "$stage.tar.gz"' EXIT
payload="$stage"
if [[ -n "$release_tag" ]]; then
    payload="$stage/media-gateway-$release_tag-linux-amd64"
    mkdir "$payload"
fi
# Explicit flags retain normal VCS metadata and avoid ambient build flag injection.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 GOFLAGS= go build \
    -buildvcs=true -o "$payload/media-gateway" ./cmd/media-gateway
install -m 0644 deploy/config.toml.example deploy/media-gateway.service \
    deploy/nginx.conf CHANGELOG.md LICENSE "$payload/"
# One source for the offline entrypoint, also checked against release assets.
install -m 0644 deploy/README.bundle.md "$payload/README.md"
install -m 0755 scripts/smoke-deployment.py "$payload/"
# Include referenced operator authorities for offline use.
mkdir "$payload/docs"
install -m 0644 docs/{index,release,deployment,configuration,logging,toolchain,consumer-api,architecture,security,product-completion,roadmap,video-qualification}.md "$payload/docs/"
(
    cd "$payload"
    find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS
    sha256sum --check --strict SHA256SUMS
)
(
    cd "$stage"
    # Stable archive metadata/order; this is not a byte-reproducible-build claim.
    archive_root=.
    if [[ -n "$release_tag" ]]; then
        archive_root="media-gateway-$release_tag-linux-amd64"
    fi
    tar --sort=name --mtime="@$(git -C "$root" show -s --format=%ct HEAD)" \
        --owner=0 --group=0 --numeric-owner -cf - "$archive_root" | gzip -n > "$stage.tar.gz"
)
# Refuse publication if source changed during the build.
[[ $(git rev-parse HEAD) == "$revision" && -z $(git status --porcelain --untracked-files=all) ]] || {
    echo 'Checkout changed during build' >&2; exit 1;
}
# No-replace publication avoids overwriting an earlier artifact.
# Use a copy into the destination filesystem, then an atomic hard link.
pending=$(mktemp "$out/.bundle-XXXXXXXX")
trap 'rm -rf -- "$stage"; rm -f -- "$stage.tar.gz" "$pending"' EXIT
cp -- "$stage.tar.gz" "$pending"
chmod 0644 "$pending"
ln -- "$pending" "$archive"
echo "$archive"
