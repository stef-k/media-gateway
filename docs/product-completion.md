# Product completion: convention-driven media publication

This document defines the product-completion contract tracked by [#37](https://github.com/stef-k/media-gateway/issues/37).

It deliberately distinguishes the **accepted V0 security/deployment baseline** from the **original product requirement that still needs completion**. V0 remains valid evidence: it proved a fail-closed provider/policy boundary, real-provider lifecycle revocation, hardened systemd/nginx deployment, bundle/rollback and a trusted loopback consumer seam. Product completion extends that foundation from image-preview delivery to the convention-driven image/video gateway originally intended.

Accepted baseline entering #37: `d3948b7e2e9de9135fbd353a10b371fb727e9be3`.

## Product statement

Media Gateway is a small secure publication gateway/proxy for media managed by a private provider such as Immich.

Operators publish media by filesystem convention: configured directory component names such as `public`, `public-images`, `public-videos` or a root-scoped legacy convention such as `post` identify media that may be exposed. Private media requires no negative rule. Trusted same-host consumers browse and select only currently eligible media. Public clients receive stable gateway URLs. Every public request re-fetches current provider metadata/lifecycle and re-evaluates publication policy.

Media Gateway is **not** the publication database, gallery manager, CMS, NAS browser, image processor or transcoder.

## Motivating archive model

A real archive may contain arbitrary year/trip/device organization such as:

```text
2019/
  Romania Dec 2019/
    post/
    raws/
    Samsung Note 9/
    video/

2022/
  Egypt Oct 2022/
    PHOTOS SAMSUNG A52/
    PHOTOS SAMSUNG NOTE 9/
    PHOTOS SONY A7 III/
    public-images/
    public-videos/
    VIDEO DJI/
    VIDEO GO PRO/

2023/
  TAIWAN/
    public/
    SAMSUNG NOTE 9/
    SONY A7 III RAW/
```

The desired operator workflow is intentionally simple:

```text
private media       -> leave it outside any configured publication convention
public image        -> place/copy/move it below public or public-images
public video        -> place/copy/move it below public or public-videos
legacy published    -> optionally recognize an exact scoped convention such as post
revoke publication  -> move it out of the matching convention
```

Consumer applications do not make an asset public. They may retain an asset ID or gateway URL, but delivery stops after the provider path/lifecycle no longer satisfies policy.

## Policy v2: named roots and scoped conventions

The current V0 model has a list of `allowed_roots` and one global set of rules. Product completion replaces that with named roots plus rules that may apply globally or only to selected roots.

Target configuration:

```toml
[policy]

[[policy.roots]]
name = "images"
path = "/media/archive/Images"

[[policy.roots]]
name = "art"
path = "/media/archive/ART"

# Global: applies beneath every configured root.
[[policy.rules]]
segment = "public"
media = ["image", "video"]

[[policy.rules]]
segment = "public-images"
media = ["image"]

[[policy.rules]]
segment = "public-videos"
media = ["video"]

# Scoped legacy convention: valid only below the logical images root.
[[policy.rules]]
segment = "post"
media = ["image"]
roots = ["images"]
```

### Root semantics

A root `path` is the absolute normalized POSIX path reported by the provider in asset metadata. It is **not** a Windows UNC path and it is never opened as a local filesystem path by Media Gateway.

The logical `name` is stable configuration identity used by rules and trusted consumer metadata. If the provider-visible mount path changes, operators can update one root path without changing every rule that references it.

Roots must be unique and non-overlapping. One asset belongs to at most one configured root; overlapping roots would create ambiguous authorization and therefore fail startup.

### Rule semantics

An asset is publication-eligible when:

1. its provider path is canonical and beneath exactly one configured root;
2. an exact directory component beneath that root equals `rule.segment`;
3. its normalized media type appears in `rule.media`; and
4. the rule is global (`roots` omitted) or the matched root name appears in `rule.roots`.

Rules are OR-ed within the applicable root. Omitted `roots` means all configured roots. `roots = []` is invalid. Every scoped root name must exist.

Segments continue to match at any descendant depth and remain exact literals, not substring/prefix/glob/regex matches. For example, `post` may match `2019/Romania/post/file.jpg` but never `post process`.

## Trusted consumer catalogue

The loopback-only consumer plane becomes a generic eligible-media catalogue. It does not browse the NAS directly and must never be published by nginx.

The desired experience has two paginated levels:

```text
GET /internal/collections?limit=<n>&cursor=<opaque>
GET /internal/assets?root=<logical-root>&collection=<relative-path>&limit=<n>&cursor=<opaque>
GET /internal/assets/<asset-id>
```

The exact hardened route/query contract belongs to #39, but these requirements are authoritative.

### Collections

A collection is identified by:

```text
root             = logical configured root name
collection_path  = path relative to that root
```

Only collections containing currently eligible media are visible. The consumer never receives provider absolute roots, NAS paths or provider URLs.

Consumers may reconstruct a year/trip/folder tree from safe relative collection paths, for example:

```text
images
  2019/Romania Dec 2019/post
  2022/Egypt Oct 2022/public-images
  2022/Egypt Oct 2022/public-videos
  2023/TAIWAN/public
```

Media Gateway still remains stateless; this is derived authorization/catalogue data rather than persistent gallery state.

### Paginated asset browsing

A collection may contain thousands of files. Asset browsing must always be bounded and paginated.

- retain a small default page size around the current 25;
- hard maximum 100;
- cursors remain opaque and bounded;
- consumers never receive an unbounded directory dump;
- provider pagination/filtering is an internal implementation detail;
- internal scanning/filling across provider pages must itself have a hard candidate/page/time budget;
- provider search predicates may optimize discovery but never grant publication permission.

Consumer-visible pagination should describe the **eligible** browse operation rather than forcing clients to understand private candidates skipped by policy. If a bounded internal scan cannot fill a requested page before its work budget is exhausted, return safe continuation state rather than scan indefinitely.

Immediately before #39 implementation, re-verify the supported Immich structured-search contract and determine whether it can narrow candidate discovery efficiently by path/root/collection. `publication` policy remains authoritative regardless.

### Asset projection

The target trusted asset projection contains only safe consumer data:

```text
id
media_type                    image | video
root                          logical root name
collection_path               relative to the root
filename                      basename/display metadata only
width / height                nullable
duration                      nullable, primarily video
file_created_at
local_date_time
latitude / longitude          validated nullable pair
preview_path                  /media/<id>/preview
original_path                 /media/<id>/original
```

Do not serialize absolute provider/NAS paths, provider URLs, credentials, library/owner identifiers, people/albums, raw EXIF or raw provider JSON.

### Coordinates

Coordinates are part of the normal trusted catalogue requirement. Product completion removes the need for a consumer feature flag.

Continue the accepted safety model: decode and validate only latitude/longitude as a pair, preserve nullable unknowns, reject malformed/non-finite/out-of-range provider data, never expose unrelated EXIF, and never log coordinates.

## Public media representations

The product has two required fixed representations.

### Preview

```text
GET  /media/<asset-id>/preview
HEAD /media/<asset-id>/preview
```

Preview is a provider-generated representation used for browse/picker/grid/poster UX. The accepted image preview contract remains useful. Video preview/poster behavior must be re-verified and qualified in #41.

Preview is **not** the definition of the published asset and is not a permanent quality ceiling.

### Original

```text
GET  /media/<asset-id>/original
HEAD /media/<asset-id>/original
```

Original means provider original bytes after current policy/lifecycle reauthorization. It must never silently mean preview, a browser-safe derivative or provider playback/transcoding output.

If a deliberately published original is JPEG, HEIC, RAW, MP4 or another provider-supported source format, `/original` returns that format. Downstream consumers may resize, transform, cache or create derivatives according to their own requirements.

Original delivery does not claim metadata sanitization. An original file's EXIF/GPS/etc. are part of those bytes. Operators who require metadata-minimal browser representations should use a qualified derivative such as preview or later #30 profiles.

## Authorization on every public request

For preview and original alike:

1. validate exact route/method/ID and bounded request headers;
2. fetch current provider metadata;
3. require active lifecycle state;
4. resolve the asset to one configured logical root;
5. evaluate exact global/root-scoped convention and media type;
6. only then request the fixed provider media representation;
7. allowlist response semantics and stream without storage fallback.

Asset IDs, catalogue selections and stored consumer references never grant publication authority.

## Video and byte ranges

Video is a first-class policy/catalogue/delivery type, not a future unrelated feature.

The final product must provide:

- eligible video discovery in the same paginated catalogue;
- safe dimensions/duration/time/coordinates where provider data supports them;
- provider-qualified video preview/poster;
- original video delivery;
- practical byte-range behavior for browsers/players.

At minimum original video must deliberately handle single byte ranges with correct `206 Partial Content`, `Accept-Ranges`, `Content-Range`, partial `Content-Length`, HEAD and unsatisfiable-range behavior. Multiple/malformed ranges must be explicitly supported or rejected rather than accidentally proxied.

Before #41 implementation, re-verify the supported/deployed Immich version for original download, video preview/poster, range behavior, permissions, redirects and any playback endpoint. A provider playback/transcoding route may become a separate explicit representation if evidence requires it, but it must never redefine `/original` silently.

## Large-media streaming lifetime

The V0 preview server uses a short absolute request/write lifetime appropriate to bounded previews. Product-complete original images and videos need a different model.

Separate:

- hard-bounded route/authorization/provider-connect/header work; from
- body transfer that may legitimately exceed the old ~60/65 second limits.

Once an authorized stream is open, lifetime should be controlled by I/O inactivity, provider/transport safety bounds, client disconnect and bounded service shutdown rather than a short absolute wall-clock deadline. nginx must mirror the accepted model and forward only explicitly supported headers such as Range.

Long streaming must not become infinite stalled streaming.

## Security/deployment invariants retained from V0

Product completion must not weaken:

- fail-closed startup and request behavior;
- exact path/media authorization;
- lifecycle revalidation;
- fixed configured Immich authority and separate credential;
- no ambient proxy receiving provider credentials;
- no direct NAS mount or filesystem read;
- no caller-selected provider URL/path;
- no absolute provider paths in public/consumer responses;
- loopback-only Go listener;
- nginx as the only public boundary;
- `/internal/` inaccessible through nginx;
- unprivileged systemd identity and zero capabilities;
- sanitized application/journald vs nginx access-log split;
- deterministic bundle, smoke, upgrade and rollback discipline.

## Relationship to #30

[#30](https://github.com/stef-k/media-gateway/issues/30) remains a later optional optimization for additional fixed browser-safe thumbnail/normal/large image representations.

It is not a replacement for the required `/original` route and it does not block #37 unless future implementation evidence establishes a concrete dependency.

## Implementation order

The deterministic product-completion lane is:

```text
#38 policy v2
  -> #39 paginated generic catalogue
  -> #40 original image delivery
  -> #41 video + ranges + long-stream behavior
  -> #42 final docs/bundle/real-host qualification
  -> close #37
```

[#38](https://github.com/stef-k/media-gateway/issues/38) owns named roots/global-and-scoped rules.

[#39](https://github.com/stef-k/media-gateway/issues/39) owns paginated collection/media browsing, image+video projection and normal nullable coordinates.

[#40](https://github.com/stef-k/media-gateway/issues/40) owns policy-checked original image bytes.

[#41](https://github.com/stef-k/media-gateway/issues/41) owns video catalogue completion, preview/poster, original ranges and large-media streaming lifetime.

[#42](https://github.com/stef-k/media-gateway/issues/42) owns final documentation/config/bundle/smoke reconciliation and exact real-host qualification.

## Deployment boundary

The accepted V0 installation on M6 is valuable qualification infrastructure, but the motivating `media.stefk.me`/Cloudflare production cutover in `stef-k/server-migration#10` should wait for #37. Deploying the preview-only slice now would reproduce the product drift this design is correcting.

## Explicit non-goals

Unless later evidence creates a separate issue, #37 does not add:

- a publication database or per-asset public flag;
- NAS mounting or generic directory/file server behavior;
- gallery/editor UI;
- WordPress/Wayfarer-specific persistence;
- arbitrary width/height/quality/crop transformation APIs;
- gateway image transcoding;
- adaptive streaming/HLS/DASH framework;
- video transcoding merely to provide originals;
- public consumer catalogue authentication/platform;
- cache or metrics stacks.

## Product-complete acceptance

Media Gateway is complete against the original requirement when an operator can define named provider roots plus global/root-scoped publication conventions, trusted consumers can safely page through eligible image/video collections and select media without seeing private provider topology, and public preview/original requests reauthorize current lifecycle/policy before streaming images or range-capable video through the hardened nginx boundary.