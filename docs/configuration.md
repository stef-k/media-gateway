---
title: "Configuration and operator quick start"
---

# Configuration and operator quick start

## Get started

Use a reachable private Immich instance (deployed validation baseline: 3.2.0), an
account that can read the intended assets, and a dedicated non-administrator API
key with `asset.read`, `asset.view`, and `asset.download`. First
[download and verify the Linux amd64 GitHub Release](release.md#download-and-verify-a-release),
then work from its extracted directory. No Go toolchain or source checkout is needed.
Python 3 is needed only for smoke tooling; systemd and nginx are the reference
production host integration.

Create a protected configuration directory outside Git. Provision the key as a
regular owner-readable file with **no group/other permissions** (for example
0400), readable by the process identity. Never put the token in TOML, shell
arguments or environment variables. Follow [installed ownership](deployment.md#filesystem-layout)
when moving from a local run to a service.

This minimal single-root example uses exactly the same schema as the
[full multi-root template](https://github.com/stef-k/media-gateway/blob/main/deploy/config.toml.example):

```toml
[server]
# Numeric loopback only; nginx owns external access.
listen = "127.0.0.1:2290"

[provider]
type = "immich"
# Adapt to the private provider origin reachable by this process.
base_url = "http://127.0.0.1:2283"
# Replace with an absolute protected credential path readable by this identity.
api_key_file = "/etc/media-gateway/immich.key"
# Bounds metadata/search/preview and original header acquisition.
request_timeout = "15s"

[[policy.roots]]
name = "photos"
# Provider-reported POSIX metadata path, never a local mount or Windows UNC path.
path = "/library/photos"

[[policy.rules]]
# Exact component anywhere beneath the root, not public-old or public_backup.
segment = "public"
media = ["image", "video"]
```

One root is sufficient for one provider namespace sharing publication conventions.
Use multiple non-overlapping named roots when archives have separate namespaces;
use root-scoped rules when a root-specific convention should apply only to one
namespace. The full example includes global `public`, `public-images`,
`public-videos`, and `post` scoped to `photos`. Exact originals require explicit source-metadata opt-in and include source EXIF/GPS.

Adapt and save the TOML, then start in the foreground:

```sh
# Run from the verified, extracted release directory.
./media-gateway -version
./media-gateway -config /absolute/path/to/config.toml
```

Startup validates TOML/key before binding; it does not probe provider readiness.
Use the [consumer guide](catalog-api.md#integration-walkthrough) against loopback
to find an eligible asset, then check its preview GET/HEAD. Originals return 404 unless source metadata is enabled. Confirm a
known private asset remains 404. Stop with Ctrl-C; use the
[deployment guide](deployment.md) for persistent systemd/nginx integration.

Configuration is loaded once; **TOML/key changes require restart**. Unknown fields,
incorrect types and malformed TOML fail startup. Input is limited to 1 MiB and
errors identify fields/operations without quoting private values. No environment,
working-directory or home-directory configuration discovery occurs.

## Connectivity and credential

Connectivity and credential requirements:

- `server.listen`: numeric loopback IP, including IPv6, and port 1..65535. No DNS resolution, wildcard binds, zone identifiers or ephemeral ports.
- `provider.type`: exactly `immich` until another provider creates a real requirement.
- `provider.base_url`: required trusted HTTP(S) origin; reject userinfo, query, fragment, non-root path and invalid ports.
- `provider.request_timeout`: required positive Go duration for bounded provider setup/metadata work. Established originals instead use fixed 60-second per-I/O inactivity bounds.
- `provider.api_key_file`: absolute normalized host file path to the separately protected credential.

The credential is intentionally separate from `Config`. Never log the credential or the whole configuration because configuration contains private topology.

## Installed ownership and startup state

Follow [deployment](deployment.md#service-account-and-installation). The reference deployment uses a root-controlled `/etc/media-gateway`, TOML readable but not writable by the service, and a restrictive separate key readable only by the required service/admin identities.

TOML and the key are loaded once before binding. **Changes require service restart**. There is no hot reload or `ExecReload`.

## Source-metadata privacy

```toml
[privacy]
# Privacy-preserving default; the table and field are both optional.
expose_source_metadata = false
```

Omitted or false keeps `file_created_at`, `local_date_time`, `latitude`,
`longitude` and `original_path` present as null in public asset JSON. Catalog
requests use `withExif=false`; hidden timestamps and EXIF are not decoded or
validated, even if unexpectedly returned. Width, height and duration still validate.
Image/video original GET/HEAD returns fixed 404 before provider access or Range
parsing. Qualified previews/posters remain available.

Set true only to deliberately expose validated capture/local timestamps and
coordinates and enable exact originals. Original bytes may contain additional
embedded EXIF/GPS, camera/device or container metadata; they are never sanitized.
The [consumer matrix](catalog-api.md#source-metadata-privacy) defines the fields.
This is not an anonymity mode: filenames and collection identity remain visible.

Configuration is immutable until restart. Unknown/unsupported fields and wrong
types fail strict decoding; failure returns zero configuration and no credential.
No configuration/schema version or compatibility aliases exist.

## Publication policy

Publication policy uses named provider roots and global or root-scoped publication rules.

Configuration shape:

```toml
[policy]

[[policy.roots]]
name = "photos"
path = "/library/photos"

[[policy.roots]]
name = "art"
path = "/library/art"

# Global convention: applies below every root.
[[policy.rules]]
segment = "public"
media = ["image", "video"]

[[policy.rules]]
segment = "public-images"
media = ["image"]

[[policy.rules]]
segment = "public-videos"
media = ["video"]

# Root-scoped publication convention.
[[policy.rules]]
segment = "post"
media = ["image"]
roots = ["photos"]
```

### Root paths

`path` is the absolute normalized POSIX path **reported by the provider in asset metadata**. It is not a Windows UNC path such as `\\example-nas\share\photos`, not a host NAS mount and never a local path opened by Media Gateway.

`name` is a unique stable logical identifier: 1..64 ASCII lowercase letters,
digits and hyphens, with alphanumeric first/last characters. Comparison is exact.
Uppercase, whitespace, Unicode, underscores and leading/trailing hyphens fail.

Paths must already be canonical; validation never repairs them. `/`, relative
paths, dot components, repeated/trailing separators, backslashes, control characters,
invalid UTF-8 and surrounding whitespace fail. Duplicate paths and ancestor/descendant
overlap fail in either declaration order. `/Images` and `/Images-old` do not overlap.
The evaluator defensively denies zero or multiple root matches.

### Rule scoping

Rules remain OR-ed exact conventions.

- omitted `roots` -> applies to every configured root;
- `roots = ["photos"]` -> applies only beneath that named root;
- explicit `roots = []` -> invalid;
- unknown/duplicated root references -> invalid;
- segment matching remains exact component equality at any descendant depth;
- `post` never matches `post process`;
- media is a non-empty list of unique exact `image`/`video` values.

One segment plus one scope may occur only once, even with different media lists.
Scope root order has no meaning. A global rule and a scoped rule are distinct,
even when the latter lists all current roots. Different scoped sets (including
partially overlapping sets) are valid and OR together. There are no deny rules
or precedence rules. Combine media in a single rule for an identical segment/scope.

`publication.Evaluate` returns eligibility and `Match{RootName, CollectionPath}`.
For root `/library/photos` and asset
`/library/photos/2026/example-trip/post/example.jpg`, the context is `photos` and
`2026/example-trip/post`. Only directory components strictly beneath the root and above
the basename can match, at any depth. Malformed metadata denies without repair.
Denial returns zero context. Current consumer JSON exposes both logical fields after successful authorization.

## Representation and lifetime boundaries

When source metadata is enabled, image/video originals retain embedded source metadata. Preview is a separately
qualified provider-generated web representation; the gateway does not rewrite it.
Video originals accept single byte ranges; image originals and previews ignore
Range. All conditionals are unsupported. See [media behavior](architecture.md#video-delivery).
`provider.request_timeout` bounds metadata/search/preview bodies and original
headers; dial/TLS are additionally capped at five seconds. Established originals
refresh 60-second read/write inactivity deadlines per I/O, with client cancellation
and a 10-second shutdown drain. Ordinary responses keep the 65-second write default;
nginx uses 70-second read and 65-second send inactivity bounds.

## Provider permissions

The dedicated non-administrator key requires `asset.read` (metadata/search), `asset.view` (preview) and `asset.download` (original). No write permission is required. Original uses only GET `/api/assets/<UUIDv4>/original` with no query, preserving Immich source semantics. See the [reviewed provider contract](deployment.md#reviewed-original-image-contract).

## Development validation

With the reviewed toolchain, run the normal repository baseline:

```sh
test -z "$(gofmt -l $(git ls-files '*.go'))"
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/media-gateway
git diff --check
```

Strict TOML decoding and fail-closed startup are mandatory.
