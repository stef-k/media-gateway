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

`internal/publication.Eligible(policy, originalPath, media)` implements the pure
policy decision using the `config.Policy` returned by `config.Load`. It accepts
only canonical absolute POSIX asset paths and normalized `image` or `video` media
values. It does not contact a provider, access storage, log metadata or deliver
bytes. Callers must not mutate the policy concurrently with evaluation.

Only exact directory components strictly beneath an allowed root and above the
asset basename can grant eligibility. Any matching rule that permits the media
type is sufficient. Roots share global rules, so nested roots form a union:
any qualifying root can grant eligibility, independently of root order. Root `/`
uses the same semantics, with the leading separator as the containment boundary.

This is a policy input, not a public filesystem mapping. Media Gateway never converts the provider path into an nginx alias or public URL.

A production version may support only a subset of the media types declared by policy. For example, V0 targets images first; video remains denied until the video-delivery slice is implemented and validated.

### Consumer applications

Consumers such as WordPress may store a provider asset reference, alt text, captions or presentation metadata. Consumer state never grants publication permission.

A request for a consumer-referenced asset must still pass the gateway's current provider-root, path-segment and media-type checks. A compromised consumer must not be able to use a private asset ID to bypass policy.

## V0 request flow

The implemented public image request flow is:

```text
GET or HEAD /media/<asset-id>/preview
          |
          v
validate route/method/identifier/variant
          |
          v
query Immich for current asset metadata
          |
          v
asset path under configured allowed root?
          | no -> 404
          v yes
contains an exact eligible directory segment?
          | no -> 404
          v yes
provider media type permitted by that rule and implemented by gateway?
          | no -> 404
          v yes
request an approved provider representation/preview
          |
          v
validate bounded upstream response
          |
          v
stream public response
```

Every delivery request re-evaluates policy. A file moved out of an eligible path therefore becomes unavailable once the provider reflects the new metadata; there is no second publication database to synchronize.

## Public URLs

V0 does not treat provider asset identifiers as secrets. Knowledge of an identifier must never be sufficient for publication; the gateway always rechecks policy.

The implemented stable route is:

```text
/media/<asset-id>/preview
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

Trusted same-host consumers use `GET /internal/assets` for bounded eligible-image
pages and `GET /internal/assets/<asset-id>` for exact eligible-image details.
The listener and consumer peer must be loopback; forwarded headers are not
identity. nginx must never publish `/internal/`, including through a local proxy.

Every candidate passes `publication.Eligible` before projection into the six safe
consumer fields. Consumer references do not authorize subsequent public delivery.
See the [consumer contract](consumer-api.md) for JSON, pagination and bounds.
There is no generic provider search, EXIF/GPS output or consumer write operation.

## Provider boundary

`internal/immich` provides a concrete metadata/preview/candidate client so provider-specific HTTP/JSON
stays outside the pure publication evaluator. Its `Asset` method returns only ID,
unchanged original path and normalized media type. It does not grant publication
itself. The public handler calls `Asset`, requires image media, evaluates
`publication.Eligible`, and only then calls `Preview`. Startup constructs one client
from validated configuration and the separately loaded key. See the
[reviewed API contract](deployment.md#reviewed-preview-contract).

`Client.SearchCandidates` adds one bounded image-candidate page or an exact
UUIDv4 candidate lookup through the same private transport. Candidates retain
private provider paths, nullable dimensions and capture/local times internally.
They are **not publication-authorized**, including when a provider filter matched.
The consumer handler evaluates every candidate with `publication.Eligible` and
omits provider paths before any consumer receives a result. The adapter itself
grants no publication authority and public delivery remains independent. See the
[reviewed search contract](deployment.md#reviewed-candidate-search-contract).

The provider needs only capabilities required by current issues, initially:

- fetch asset metadata by stable provider ID;
- obtain media type;
- obtain the indexed original path/folder metadata needed for policy evaluation;
- obtain a suitable image preview/representation.

Do not build provider discovery, dynamic plugins or a generic provider SDK before a second provider establishes real requirements.

Exact Immich API endpoints must be verified against the supported Immich version at implementation time.

## Image delivery

V0 implements provider-generated preview streaming. Immich owns generation;
Media Gateway does no transcoding, original/fullsize fallback or image buffering.
The sole upstream representation is `/api/assets/{id}/thumbnail?size=preview`.
All redirects are rejected. Before public headers, require HTTP 200, exactly
`image/jpeg` or `image/webp`, and a known positive `Content-Length` no larger than
16 MiB. Encoded, chunked and partial representations are rejected.

GET streams the validated representation. HEAD performs the same fresh metadata,
policy and preview-header checks, then closes the provider body without draining
it. Both construct only content type/length, `X-Content-Type-Options: nosniff` and
`Cache-Control: no-store` (plus standard HTTP framing/date). Provider headers are
never forwarded. Queries and caller headers do not select upstream behavior.
Range and conditional headers are ignored; no 206, ETag or 304 support exists.

Every request revalidates authorization; no-store prevents a gateway-supported
cache from bypassing revocation once Immich reports the changed path. It cannot
recall bytes already received or stop an already-authorized in-flight response.
A short/failed stream aborts the public connection or HTTP stream; headers already
sent cannot be replaced with a 502. No error text is appended to image bytes.

#17 supplies deterministic software evidence. #18 remains the deployed-Immich
privacy and quality gate before production use.

A representation may be qualified for production public delivery only after tests prove:

- acceptable visual quality for the intended web use;
- no sensitive EXIF/GPS metadata is present in the delivered derivative;
- content type is validated;
- response size/streaming is bounded appropriately;
- private originals are not accidentally exposed.

If provider previews do not meet those requirements, an explicit image-transformation slice may add safe re-encoding/metadata stripping. Do not add ImageMagick/libvips/transcoding dependencies preemptively.

Original-file delivery is off by default and is not a V0 requirement.

## Video delivery

Video is deliberately later work. Correct public video delivery may require range requests, content-length/range semantics, larger timeouts, codec/container behavior and different cache policy.

Until that slice is implemented, video requests fail closed even if the configured path rule marks the asset eligible.

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

[policy]
allowed_roots = ["/media/archive"]

[[policy.rules]]
segment = "public"
media = ["image", "video"]

[[policy.rules]]
segment = "public-images"
media = ["image"]

[[policy.rules]]
segment = "public-videos"
media = ["video"]

[delivery]
allow_original = false
```

The [committed example](../deploy/config.toml.example) and [configuration contract](configuration.md) describe the validated schema, including the required provider request timeout and preview image variant. `public_base_url` is optional.

Invalid policy must fail startup rather than silently widen access. Rules use exact normalized directory-segment equality; arbitrary regex/glob policy is not required for V0.

## State

Media Gateway has no application database in V0.

Allowed state is limited to normal process/runtime state and, if later justified, disposable derived-media cache. A cache must never become publication authority; policy must still be checked before serving cached content unless an explicitly designed cache contract proves equivalent revocation semantics.

## Dependencies

Prefer Go's standard library. The expected V0 problem is small enough that a web framework, dependency injection container, ORM, message broker, scheduler or container runtime is unnecessary.

A small TOML parser is an acceptable dependency if chosen deliberately. Additional dependencies require concrete value and should stay auditable.

## Failure behavior

- Provider/auth/metadata/representation failure before headers: fixed `502` with
  `media unavailable` body; no direct-storage fallback. Mid-stream errors abort.
- Invalid/missing/private/unsupported assets or missing previews: fixed `404` with
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

- video/range delivery;
- optional local derivative cache;
- optional image re-encoding/format variants;
- signed or opaque public URLs if evidence justifies them;
- additional private consumer integrations;
- another provider, once a real second provider exists.

These are not V0 requirements and must not add complexity to the first secure image path without evidence.
