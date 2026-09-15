# Architecture

## Purpose

Media Gateway is a small, fail-closed HTTP publication boundary between a private media provider and public consumers.

The initial deployment uses Immich as the provider. Immich remains private and indexes a read-only external archive. Media Gateway is the only component allowed to turn a provider asset into an Internet-deliverable response, and only after re-evaluating publication policy from current provider metadata.

The service is intentionally smaller than the applications consuming it. It has no media-management UI, no publication database, no direct NAS access and no generic proxy behavior.

## Initial deployment

```text
                                  PRIVATE

                     NAS curated photo archive
                                |
                                v
                              Immich
                         127.0.0.1:2283
                                |
                         private API key
                                |
                                v
                      +-------------------+
                      |   Media Gateway   |
                      | 127.0.0.1:2290   |
                      +---------+---------+
                                |
                       public routes only
                                |
                                v
                              nginx
                                |
                       Cloudflare / HTTPS
                                |
=============================== | ===============================
                              Internet
                                |
                                v
                        media.stefk.me
```

The exact deployment may differ for other installations. The architectural requirements are:

- the provider is not directly Internet-exposed by Media Gateway;
- the V0 gateway binds only to a numeric loopback address; other network boundaries require an explicit architecture revision;
- the public reverse proxy exposes only public delivery routes;
- provider credentials stay on the trusted host and are never sent to consumers.

## Trust boundaries

### Media provider

Immich owns indexing, metadata extraction, asset identity and media retrieval. It may see both private and publication-eligible assets.

Media Gateway must therefore treat provider reachability as **capability to inspect**, not permission to publish.

### Publication policy

The initial deployment uses provider-indexed path metadata as the single publication switch.

Configured root:

```text
/media/archive
```

Eligible exact directory segments:

```text
public          -> images and videos eligible
public-images   -> images eligible
public-videos   -> videos eligible
```

Anything else is private.

`internal/publication.Evaluate(policy, originalPath, media)` implements the pure
policy decision using the `config.Policy` returned by `config.Load`. It accepts
only canonical absolute POSIX asset paths and normalized `image` or `video` media
values. It does not contact a provider, access storage, log metadata or deliver
bytes. Callers must not mutate the policy concurrently with evaluation.

Only exact directory components strictly beneath an allowed root and above the
asset basename can grant eligibility. Any matching rule that permits the media
type within its global or named-root scope is sufficient (OR semantics, no
precedence). Named roots must have unique names and canonical non-overlapping
paths; `/` is invalid. The evaluator defensively denies zero or multiple matching
roots. It returns only `Match{RootName, CollectionPath}` plus eligibility, with
zero context on denial. The collection is the root-relative parent path without
leading/trailing slashes. The trusted catalogue projects this context only after successful evaluation.

This is a policy input, not a public filesystem mapping. Media Gateway never converts the provider path into an nginx alias or public URL.

A production version may support only a subset of the media types declared by policy. Both image and video preview/original are implemented; #41 real-provider qualification remains a merge gate.

### Consumer applications

Consumers such as WordPress may store a provider asset reference, alt text, captions or presentation metadata. Consumer state never grants publication permission.

A request for a consumer-referenced asset must still pass the gateway's current provider-root, path-segment and media-type checks. A compromised consumer must not be able to use a private asset ID to bypass policy.

## Current media request flow

The implemented public image/video request flow is:

```text
GET or HEAD /media/<asset-id>/preview
GET or HEAD /media/<asset-id>/original
          |
          v
validate route/method/identifier/variant
          |
          v
query Immich for current asset metadata
          |
          v
explicit isTrashed=false and isOffline=false?
          | unavailable -> 404; missing/invalid fields -> 502
          v active
asset path under configured allowed root?
          | no -> 404
          v yes
contains an exact eligible directory segment?
          | no -> 404
          v yes
provider media type permitted by that rule and implemented by gateway?
          | no -> 404
          v yes
parse Range only for authorized video original (malformed -> 400)
          |
          v
request the fixed authorized preview or original
          |
          v
validate bounded upstream response
          |
          v
stream public response
```

Every delivery request checks current provider lifecycle availability before evaluating
policy. Immich external-library moves can retain an old eligible path on a
trashed/offline record; that record now denies delivery. An active record at a
new ineligible path also denies. There is no second publication database to synchronize.

## Public URLs

V0 does not treat provider asset identifiers as secrets. Knowledge of an identifier must never be sufficient for publication; the gateway always rechecks policy.

The implemented stable image/video routes are:

```text
/media/<asset-id>/preview
/media/<asset-id>/original
```

without a publication database or reversible token scheme.

Signed or opaque URLs may be added later only if they solve a demonstrated abuse, privacy or integration requirement. They must remain an additional control rather than replacing policy evaluation.

Provider paths and NAS paths must never appear in public URLs.

## Public HTTP surface

The V0 public surface is intentionally narrow:

- `GET` and `HEAD` only for implemented media routes;
- no health/readiness endpoint;
- bounded headers, request sizes and upstream timeouts;
- no public search;
- no public provider metadata endpoint;
- no configuration endpoint;
- no arbitrary fetch/proxy endpoint;
- no directory listing.

Private/missing/invalid/unauthorized assets should normally be indistinguishable through a `404` response.

## Consumer/control surface

Trusted same-host consumers browse paginated image/video collections and exact
collection assets, with independently reauthorized detail. The listener and TCP
peer must be loopback; forwarded headers are not identity. nginx never publishes
`/internal/`. See the [consumer contract](consumer-api.md) for the full allowlist.

Collection identities come from Policy v2 and are deduplicated within each page,
with at-least-once discovery across pages. Stateless gateway HMAC cursors bind the
query/policy and invalidate on restart. Each request has at most eight provider
calls, a 30-second deadline and 512 KiB buffered JSON output. Provider page sizes
never exceed remaining output slots. No catalogue database or folder-view API is used.

Assets expose logical root/relative collection, path basename, image/video type,
nullable dimensions and integer `duration_ms`, validated times and always-present
nullable coordinates. Images and videos have non-null preview and original
paths without provider representation probes. Consumer references do not
authorize subsequent detail or public delivery. Raw EXIF remains private.

## Provider boundary

`internal/immich` provides a concrete metadata/preview/original/candidate client so provider-specific HTTP/JSON
stays outside the pure publication evaluator. Its `Asset` method returns only ID,
unchanged original path and normalized media type. It does not grant publication
itself. The adapter requires explicit boolean `isTrashed=false` and
`isOffline=false` before returning metadata. Either true returns `ErrMissing`;
missing, null or wrongly typed lifecycle fields return `ErrMetadata`. Lifecycle
state stays within the provider boundary; `publication.Evaluate` remains pure.
The public handler calls `Asset`, requires supported image/video media, evaluates
`publication.Evaluate`, and only then calls the fixed `Preview`, `Original` or `VideoOriginal` operation. Startup constructs one client
from validated configuration and the separately loaded key. See the
[reviewed API contract](deployment.md#reviewed-preview-contract).

`Client.SearchCandidates` performs one bounded structured metadata search page
for gateway-owned discovery, collection assets or exact UUIDv4 detail. Discovery
uses `withExif=false` and decodes only lifecycle/path/media facts. Asset projection
uses `withExif=true`, validates dimensions/duration/times, and retains only the
validated nullable coordinate pair. Unavailable candidates are omitted; malformed
consumed metadata fails the request. Provider path/media/lifecycle filters are
optimizers only: case/accent overmatches still pass exact `publication.Evaluate`.
The handler fills pages within its finite budget and requires exact root/parent
membership for collection assets. See the
[reviewed search contract](deployment.md#reviewed-candidate-search-contract).

The provider needs only capabilities required by current issues, initially:

- fetch asset metadata by stable provider ID;
- obtain media type;
- obtain the indexed original path/folder metadata needed for policy evaluation;
- obtain a suitable image preview/representation.

Do not build provider discovery, dynamic plugins or a generic provider SDK before a second provider establishes real requirements.

Exact Immich API endpoints must be verified against the supported Immich version at implementation time.

## Preview delivery

Images and videos use the same provider-generated still preview streaming. Immich owns generation;
Media Gateway does no transcoding, original/fullsize fallback or image buffering.
The fixed preview upstream representation is `/api/assets/{id}/thumbnail?size=preview`.
All redirects are rejected. Before public headers, require HTTP 200, exactly
`image/jpeg` or `image/webp`, and a known positive `Content-Length` no larger than
16 MiB. Encoded, chunked and partial representations are rejected.

GET streams the validated representation. HEAD performs the same fresh metadata,
policy and selected representation-header checks, then closes the provider body without draining
it. Both construct only content type/length, `X-Content-Type-Options: nosniff` and
`Cache-Control: no-store` (plus standard HTTP framing/date). Provider headers are
never forwarded. Queries and caller headers do not select upstream behavior.
Range and conditional headers are ignored; no 206, ETag or 304 support exists.

Every request revalidates authorization; no-store prevents a gateway-supported
cache from bypassing revocation once Immich reports an ineligible path or trashed/offline state. It cannot
recall bytes already received or stop an already-authorized in-flight response.
A short/failed stream aborts the public connection or HTTP stream; headers already
sent cannot be replaced with a 502. No error text is appended to image bytes.

#17 supplies deterministic software evidence. Accepted #18 supplies real Immich
3.2.0 representative preview privacy/quality and live lifecycle-revocation evidence.
`/preview` is the first accepted representation, not a permanent quality ceiling;
#30 tracks post-V0 fixed safe representation profiles.

A preview derivative may be qualified for production public delivery only after tests prove:

- acceptable visual quality for the intended web use;
- no sensitive EXIF/GPS metadata is present in the delivered derivative;
- content type is validated;
- response size/streaming is bounded appropriately;
- private originals are not accidentally exposed.

If provider previews do not meet those requirements, an explicit image-transformation slice may add safe re-encoding/metadata stripping. Do not add ImageMagick/libvips/transcoding dependencies preemptively.

### Original images (#40)

`Client.Original` retrieves only GET `/api/assets/<UUIDv4>/original`, with `x-api-key` and no query. `asset.download` joins the metadata/preview permissions. Current active lifecycle, Policy v2 and image type are mandatory before opening it. Originals preserve source bytes and embedded EXIF/GPS, including RAW/HEIC, with no conversion or fallback. #40 was accepted; #41 video qualification remains pending.

Validate direct 200, exactly one valid parameter-free `image/*` Content-Type and one explicit positive int64 Content-Length. Reject transfer/content encoding and Content-Range. There is no preview-size ceiling. GET streams a shared length-enforcing body; HEAD validates the provider GET then closes it. Both construct the same four public headers described above.

Originals use a separate HTTP/1 transport with no environment proxy, redirects, compression or connection reuse. A bounded 16 KiB wire-header check rejects duplicate Content-Length and transfer encoding before Go normalizes them; net/http continues to own HTTP parsing and body framing. Dial/TLS/header acquisition remain bounded. Established image/video body streams use 60-second per-I/O upstream read and downstream write inactivity deadlines. Explicit writes/flushes refresh the server deadline; client cancellation propagates upstream. Ordinary responses retain finite server defaults. Shutdown drains for 10 seconds then force-closes remaining streams.

## Video delivery

Video preview uses the existing thumbnail `size=preview` operation and JPEG/WebP validation. Video originals use only GET `/api/assets/<id>/original` without query, never `/video/playback`. Accept parameter-free `video/*` or the explicit Immich `application/mxf` exception; preserve exact bytes and source metadata.

After fresh authorization, `ParseRange` accepts at most one 128-byte value with unsigned signed-int64 decimals: explicit, open-ended or positive suffix bytes. Reject whitespace, signs, other units and multiple ranges with fixed 400 before original open. Reserialize parsed numbers before forwarding. No Range requires direct 200; a valid Range requires direct 206 or a valid unsatisfiable 416. Provider 200 for Range is 502 without fallback.

For 206, validate one `bytes start-end/total` against the exact requested interval, including EOF clipping and suffix calculation, and require length `end-start+1`. Total is positive signed-int64. Validate optional provider Accept-Ranges as a single `bytes`. Raw duplicate lengths and transfer framing are rejected before HTTP normalization for both 200 and 206. For 416, validate one `bytes */total` and unsatisfiability, discard the provider body and construct a zero-length response. HEAD uses provider GET with the same checks and closes the body.

Public video originals add `Accept-Ranges: bytes` and validated Content-Range for 206/416. Other provider headers are discarded. Image original Range remains ignored/full 200, and all previews ignore Range. Conditional/validator semantics remain unsupported. M6 Immich 3.2.0 original-range, poster privacy, long-stream and cleanup evidence is mandatory before #41 merge; source/synthetic tests do not establish it.

## Configuration

V0 uses TOML. Configuration describes policy and connectivity, not publication state.

Representative shape:

```toml
[server]
listen = "127.0.0.1:2290"
public_base_url = "https://media.example.com"

[provider]
type = "immich"
base_url = "http://127.0.0.1:2283"
api_key_file = "/etc/media-gateway/immich.key"

[[policy.roots]]
name = "images"
path = "/media/archive"

[[policy.rules]]
segment = "public"
media = ["image", "video"]

[[policy.rules]]
segment = "public-images"
media = ["image"]

[[policy.rules]]
segment = "public-videos"
media = ["video"]

```

The [committed example](../deploy/config.toml.example) and [configuration contract](configuration.md) describe the validated schema, including the required provider request timeout. Preview/original are fixed image/video routes; stale `[delivery]` fails with migration guidance. `public_base_url` is optional.

Invalid policy must fail startup rather than silently widen access. Rules use exact normalized directory-segment equality; arbitrary regex/glob policy is not required for V0.

## State

Media Gateway has no application database. The catalogue keeps only a random 32-byte HMAC key and deterministic discovery streams in process memory; no persistent cursor state or seen-set exists.

Allowed state is limited to normal process/runtime state and, if later justified, disposable derived-media cache. A cache must never become publication authority; policy must still be checked before serving cached content unless an explicitly designed cache contract proves equivalent revocation semantics.

## Dependencies

Prefer Go's standard library. The expected V0 problem is small enough that a web framework, dependency injection container, ORM, message broker, scheduler or container runtime is unnecessary.

A small TOML parser is an acceptable dependency if chosen deliberately. Additional dependencies require concrete value and should stay auditable.

## Failure behavior

- Provider/auth/metadata/representation failure before headers: fixed `502` with
  `media unavailable` body; no direct-storage fallback. Mid-stream errors abort.
- Invalid/missing/private/unsupported/trashed/offline assets or missing representations: fixed `404` with
  `not found` body. HEAD has matching error headers but no body. All use no-store.
- Provider metadata incomplete/ambiguous: deny.
- Asset outside allowed root: deny.
- No exact eligible segment: deny.
- Unsupported media type/variant: deny.
- Malformed asset ID: deny before provider access where possible.
- Configuration invalid: service does not start.
- Credential unavailable: service does not start or remains unready; never run in a broadened anonymous mode.

## Future extensions

Potential later capabilities include:

- optional local derivative cache;
- optional image re-encoding/format variants;
- signed or opaque public URLs if evidence justifies them;
- additional private consumer integrations;
- another provider, once a real second provider exists.

These are not V0 requirements and must not add complexity to the first secure image path without evidence.
