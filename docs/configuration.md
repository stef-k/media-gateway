# Configuration foundation

> **Current schema:** Policy v2 (#38) uses named roots and global/root-scoped rules. Public delivery supports image preview and original and trusted browsing supports paginated image/video collections; #40 still requires M6 qualification; #41–#42 own remaining product completion.

`internal/config.Load(filename)` reads an explicitly supplied TOML file and returns validated typed configuration, a separate credential string, and an error. It does not start a listener, contact Immich, evaluate asset authorization or deliver media. Any failure returns zero configuration and an empty credential. No environment, working-directory or home-directory configuration discovery occurs.

Use the commented [operator example](../deploy/config.toml.example) for the **current accepted implementation**. Unknown fields/tables, incorrect types and malformed TOML are rejected. Configuration input is limited to 1 MiB. Parser errors are deliberately replaced with a schema/syntax diagnostic because raw parser messages can quote private input. Validation errors identify the offending field or operation without its value.

## Connectivity and credential

These requirements remain product invariants through #37:

- `server.listen`: numeric loopback IP, including IPv6, and port 1..65535. No DNS resolution, wildcard binds, zone identifiers or ephemeral ports.
- `server.public_base_url`: optional HTTP(S) origin.
- `provider.type`: exactly `immich` until another provider creates a real requirement.
- `provider.base_url`: required trusted HTTP(S) origin; reject userinfo, query, fragment, non-root path and invalid ports.
- `provider.request_timeout`: required positive Go duration for bounded provider setup/metadata work. #41 may refine streaming lifetime separately from this request/open bound.
- `provider.api_key_file`: absolute normalized host file path to the separately protected credential.

The credential is intentionally separate from `Config`. Never log the credential or the whole configuration because configuration contains private topology.

## Installed ownership and startup state

Follow [deployment](deployment.md#service-account-and-installation). The accepted reference contract uses a root-controlled `/etc/media-gateway`, TOML readable but not writable by the service, and a restrictive separate key readable only by the required service/admin identities.

TOML and the key are loaded once before binding. **Changes require service restart**. There is no hot reload or `ExecReload`; #37 does not change that requirement.

## Current delivery and consumer configuration

Image `preview` and `original` are fixed product representations. There is no `[delivery]` section or operator-selectable delivery mode. Remove the entire obsolete section, including `allow_original` and `image_variant`, before restart. Any stale section fails with sanitized targeted migration guidance; failure returns zero configuration and an empty credential. No compatibility alias exists. Trusted catalogue coordinates are always included as a validated nullable pair; no `[consumer]` table remains. A stale `consumer.expose_coordinates` key fails startup with sanitized guidance to remove it because coordinates are now always included. Failure returns zero configuration and no credential. Other unknown fields/tables remain strictly rejected.

## Current Policy v2 (#38)

Policy v2 replaces flat `allowed_roots` with named provider roots and allows rules to be global or scoped to selected roots.

Configuration shape:

```toml
[policy]

[[policy.roots]]
name = "images"
path = "/media/archive/Images"

[[policy.roots]]
name = "art"
path = "/media/archive/ART"

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

# Root-scoped legacy convention.
[[policy.rules]]
segment = "post"
media = ["image"]
roots = ["images"]
```

### Root paths

`path` is the absolute normalized POSIX path **reported by the provider in asset metadata**. It is not a Windows UNC path such as `\\NAS\Multimedia\Images`, not a host NAS mount and never a local path opened by Media Gateway.

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
- `roots = ["images"]` -> applies only beneath that named root;
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
For root `/media/archive/Images` and asset
`/media/archive/Images/2019/Romania/post/DSC1.JPG`, the context is `images` and
`2019/Romania/post`. Only directory components strictly beneath the root and above
the basename can match, at any depth. Malformed metadata denies without repair.
Denial returns zero context. Current consumer JSON exposes both logical fields after successful authorization.

### Breaking migration from V0

Replace `policy.allowed_roots` with named `[[policy.roots]]` entries and choose
unique logical names. Existing global rules can remain global; add `roots` only
when a convention should apply under selected roots. Nested roots and `/` must be
replaced with meaningful non-overlapping namespaces.

There is no runtime alias or automatic translation. An obsolete
`policy.allowed_roots` field fails startup with a sanitized Policy v2 migration
error that identifies the field without echoing private values. All other unknown
fields remain strictly rejected. Failure returns no partial configuration or
credential. Review the new configuration and restart; there is no hot reload.

## Representation and lifetime boundaries

Original image bytes retain embedded EXIF/GPS. Preview is a separately qualified provider-generated web representation; the gateway never inspects or rewrites image metadata. Video delivery and Range remain #41 work. `provider.request_timeout` still bounds metadata/search/preview bodies. For originals it bounds response headers, with dial/TLS additionally capped at five seconds; it is not an absolute original-body timeout. The gateway retains its 60-second handler context and 65-second write timeout, and nginx retains 70-second read inactivity.

## Provider permissions

The dedicated non-administrator key requires `asset.read` (metadata/search), `asset.view` (preview) and `asset.download` (original). No write permission is required. Original uses only GET `/api/assets/<UUIDv4>/original` with no query, preserving Immich source semantics. See the [reviewed provider contract](deployment.md#reviewed-original-image-contract-40). M6 original-image qualification remains required before merge.

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

Strict TOML decoding and fail-closed startup remain mandatory throughout #37.
