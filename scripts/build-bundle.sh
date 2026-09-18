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
trap 'rm -rf -- "$stage"; rm -f -- "$stage.tar.gz"' EXIT
# Explicit flags retain normal VCS metadata and avoid ambient build flag injection.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 GOFLAGS= go build \
    -buildvcs=true -o "$stage/media-gateway" ./cmd/media-gateway
install -m 0644 deploy/config.toml.example deploy/media-gateway.service \
    deploy/nginx.conf LICENSE "$stage/"
# Keep the bundle entrypoint small; relative guide links resolve inside docs/.
cat > "$stage/README.md" <<'GUIDE'
# Media Gateway Linux bundle

Verify this bundle before use:

```sh
sha256sum --check --strict SHA256SUMS
./media-gateway -version
```

Read [install, upgrade, rollback and smoke instructions](docs/release.md).
The [documentation homepage](docs/index.md) links operator and consumer guides.
Configuration and credentials are supplied separately; templates are examples.
GUIDE
install -m 0755 scripts/smoke-deployment.py "$stage/"
# Include referenced operator authorities for offline use.
mkdir "$stage/docs"
install -m 0644 docs/{index,release,deployment,configuration,logging,toolchain,consumer-api,architecture,security,product-completion,roadmap,video-qualification}.md "$stage/docs/"
(
    cd "$stage"
    find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS
    sha256sum --check --strict SHA256SUMS
    # Stable archive metadata/order; this is not a byte-reproducible-build claim.
    tar --sort=name --mtime="@$(git -C "$root" show -s --format=%ct HEAD)" \
        --owner=0 --group=0 --numeric-owner -cf - . | gzip -n > "$stage.tar.gz"
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
