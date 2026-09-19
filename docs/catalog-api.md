---
title: "Public catalog API"
---

# Public catalog API

The public catalog exposes only currently policy-eligible media through the same
Gateway origin as media delivery. nginx proxies its explicit GET routes to the
numeric loopback process. Every catalog response grants
`Access-Control-Allow-Origin: *`: browsers use ordinary GET without cookies,
Authorization or other custom request headers. Credentialed CORS and OPTIONS are
unsupported. Current lifecycle and publication policy authorize every result.

## Integration walkthrough

Call the public Gateway origin from the application or browser. The following uses synthetic IDs and logical selectors:

```sh
# Browse a small page; URL-encode each subsequent opaque cursor unchanged.
curl --fail --get 'https://media.example.com/catalog/collections' \
  --data-urlencode 'limit=25'
```

Select one returned collection; descendants are separate collections. Use the query
key `collection` (the JSON response calls the field `collection_path`):

```sh
curl --fail --get 'https://media.example.com/catalog/assets' \
  --data-urlencode 'root=photos' --data-urlencode 'collection=2026/example-trip/public' \
  --data-urlencode 'limit=25'
curl --fail 'https://media.example.com/catalog/assets/12345678-1234-4234-8234-123456789abc'
```

Consume each page, then repeat the same operation/selectors with
`--data-urlencode "cursor=$NEXT_CURSOR"` until `next_cursor` is null. A short or
empty page can still have continuation. Restart browsing after gateway restart
invalidates a cursor; merge collections by `(root, collection_path)` and avoid
assuming a provider snapshot. Never print real catalog responses into public logs.

### From a selected asset to a public URL

Select an asset from the `/catalog/assets` response above. For example, these
fields from a synthetic asset show the default privacy mode (other fields omitted):

```json
{
  "id": "12345678-1234-4234-8234-123456789abc",
  "preview_path": "/media/12345678-1234-4234-8234-123456789abc/preview",
  "original_path": null
}
```

Copy the returned `preview_path` and use the same public origin:

```sh
# Synthetic path: replace with the selected asset's returned preview_path.
preview_path='/media/12345678-1234-4234-8234-123456789abc/preview'
curl --fail --output /dev/null "https://media.example.com$preview_path"
```

After the operator has [installed nginx and configured public HTTPS](deployment.md#operator-path),
prepend the configured public gateway origin to the same path:

```sh
# Synthetic origin: replace with your configured public origin, without a trailing slash.
gateway_origin='https://media.example.com'
# Requests https://media.example.com/media/12345678-1234-4234-8234-123456789abc/preview
curl --fail --output /dev/null "$gateway_origin$preview_path"
```

The consumer constructs this URL; catalog responses contain paths, not full
URLs. Setting an origin does not install nginx, configure DNS or enable HTTPS.

When `privacy.expose_source_metadata` is omitted or false, `original_path` is
null and public original GET/HEAD remains 404. Do not construct an original URL
from the ID to bypass that setting. When explicitly true, use the returned
non-null `original_path` in exactly the same way:

```sh
# Only with source metadata enabled; copy the selected asset's returned original_path.
original_path='/media/12345678-1234-4234-8234-123456789abc/original'
curl --fail --output /dev/null "$gateway_origin$original_path"
```

Originals preserve exact source bytes, including embedded EXIF/GPS or container
metadata. See [source-metadata privacy](#source-metadata-privacy) and
[delivery semantics](architecture.md#preview-delivery) for the detailed contracts.

A returned path is not proof that its representation is available. Each enabled
delivery rechecks current lifecycle and publication policy; storing a URL never
grants authority. Handle 404 revocation/missing representations and sanitized 502
failures at delivery. Your application owns selection persistence, captions and
presentation.

## Routes and selectors

Only GET is accepted:

```text
GET /catalog/collections?limit=<n>&cursor=<opaque>
GET /catalog/assets?root=<logical-root>&collection=<relative-path>&limit=<n>&cursor=<opaque>
GET /catalog/assets/<UUIDv4>
```

Detail accepts no query, including an empty `?`. Routes are exact, with no path cleaning or redirects. Unknown/duplicate query keys, explicit empty values, invalid encoding and malformed selectors receive the fixed 404 denial before provider access.

Asset browsing requires an exact configured logical `root` and a canonical relative POSIX `collection`: nonempty valid UTF-8, no leading/trailing slash, empty/dot/dot-dot components, backslashes or control characters. Normal query decoding applies once; selectors are never cleaned or Unicode-normalized into validity. Consumers never supply an absolute provider path.

Every active candidate passes `publication.Evaluate`. Asset browsing additionally requires exact root and parent-collection equality: descendants are separate collections. A valid known-root selector with no eligible media returns 200 with an empty page, without revealing whether a private directory exists. A stored asset reference grants no authority; detail independently rechecks current lifecycle and publication policy.

## Bounds and pagination

| Boundary | Limit |
| --- | --- |
| Default consumer page | 25 |
| Maximum consumer page | 100 |
| Raw query | 8 KiB |
| Encoded gateway cursor | 2 KiB |
| Decoded collection selector | 2 KiB UTF-8 |
| Provider calls per request | 8 |
| Overall catalog work | 30 seconds |
| Encoded consumer JSON | 512 KiB |

`limit` accepts decimal digits only, with value 1–100. Each provider page requests at most the remaining consumer output slots. All received candidates are consumed, so filling cannot skip an unconsumed tail. Sparse pages may exhaust the successful-work budget with fewer than `limit` results, including zero, and a continuation. Provider/auth/transport/malformed-metadata failures still fail the whole request; accumulated results are never returned as partial success. Disconnect cancels outstanding work.

Cursors are gateway-owned versioned HMAC-SHA256 tokens, authenticated using a random 32-byte process-start key. Startup fails if randomness is unavailable. There is no cursor file, configuration, database or session cache. The payload contains only version, operation, query/policy fingerprint, discovery-stream index and opaque provider continuation. No provider paths or credentials are included. MAC comparison is constant-time.

Wrong version/kind/fingerprint, invalid MAC, malformed or oversized tokens fail before provider I/O. Asset tokens bind the exact decoded `(root, collection)` selector and policy; collection tokens bind deterministic effective discovery streams. Cursors never grant publication authority and are never logged. Restart invalidates existing tokens: consumers restart browsing. Consumers cannot directly submit provider cursors.

## Collections

```json
{
  "collections": [
    {"root": "photos", "collection_path": "2026/example-trip/public-images"}
  ],
  "next_cursor": null
}
```

Collection identity is exactly `(root, collection_path)`, derived only from the successful publication policy result for an active eligible asset. No collection counts are returned. `next_cursor` is always present as a string or `null`.

Discovery combines applicable global and root-scoped rules by exact segment, unions media sets, and traverses streams sorted by logical root name then segment. Provider root-prefix and segment-shaped filters only narrow candidates. Immich's case/accent-insensitive matching never replaces exact policy evaluation. Discovery uses `withExif=false` and decodes only lifecycle/path/media facts; unrelated dimensions, times or EXIF cannot fail discovery.

Collection discovery is **at least once**: identities are deduplicated within a response page, but may recur on later pages. Consumers must idempotently merge by `(root, collection_path)`. Stable accessible candidate streams eventually expose their eligible collections. Immich offset pagination is not a snapshot; library mutations can also cause repeats/skips. There are no persistent seen sets or cursor sessions. Immich folder-view endpoints are not used: their unpaginated, timeline-specific behavior does not satisfy this contract.

## Asset JSON

Asset lists return `{"assets": [...], "next_cursor": null}`; detail returns one asset directly. Every asset has exactly these fields (synthetic privacy-off defaults below):

```json
{
  "id": "12345678-1234-4234-8234-123456789abc",
  "media_type": "image",
  "root": "photos",
  "collection_path": "2025/example-trip/post",
  "filename": "DSC01234.JPG",
  "width": 6000,
  "height": 4000,
  "duration_ms": null,
  "file_created_at": null,
  "local_date_time": null,
  "latitude": null,
  "longitude": null,
  "preview_path": "/media/12345678-1234-4234-8234-123456789abc/preview",
  "original_path": null
}
```

`media_type` is `image` or `video`. `filename` is the basename of the validated provider path, never an arbitrary provider display filename. Width, height and `duration_ms` are explicit nullable nonnegative safe JSON integers (maximum 9007199254740991). Duration is **milliseconds**, so a 23.8-second video has `duration_ms: 23800`. Zero is valid; null means unknown. When source metadata is enabled, both required time strings are validated as RFC3339 with optional fractional seconds and preserved; local wall time is not converted to another timezone.

An example video page with source metadata enabled (synthetic values) uses the same schema. A detail request
returns the asset object directly, without the page envelope:

```json
{
  "assets": [{
    "id": "12345678-1234-4234-8234-123456789abd",
    "media_type": "video",
    "root": "photos",
    "collection_path": "2026/example-trip/public",
    "filename": "clip.mp4",
    "width": 1920,
    "height": 1080,
    "duration_ms": 23800,
    "file_created_at": "2026-06-01T10:00:00Z",
    "local_date_time": "2026-06-01T12:00:00Z",
    "latitude": null,
    "longitude": null,
    "preview_path": "/media/12345678-1234-4234-8234-123456789abd/preview",
    "original_path": "/media/12345678-1234-4234-8234-123456789abd/original"
  }],
  "next_cursor": null
}
```

### Source-metadata privacy

The optional startup setting `[privacy] expose_source_metadata = false` controls
this fixed surface. It cannot be selected by HTTP callers.

| Field or behavior | Omitted / false | True |
| --- | --- | --- |
| `file_created_at` | Present, null | Validated capture timestamp |
| `local_date_time` | Present, null | Validated local timestamp, unchanged timezone meaning |
| `latitude`, `longitude` | Both present, null | Validated nullable pair |
| `original_path` | Present, null | `/media/<id>/original` |
| Original image/video GET/HEAD, including Range | Fixed 404, zero provider original fetches | Existing exact-byte, range and streaming contract |
| `preview_path` | `/media/<id>/preview` | Same |
| Asset list/detail `withExif` | false | true |
| Collection discovery `withExif` | false | false |
| `id`, `media_type`, `root`, `collection_path`, `filename` | Existing semantics | Same |
| `width`, `height`, `duration_ms` | Validated nullable integers | Same |

While off, hidden capture/local timestamps and EXIF are ignored rather than
validated, including malformed values within otherwise valid bounded JSON.
With true, timestamps retain RFC3339 validation and original precision. Coordinates
are both absent/null or a finite numeric pair: latitude `[-90,90]`, longitude
`[-180,180]`. Zero and inclusive boundaries are valid. Partial pairs,
null/numeric mismatches, nonnumeric/non-finite and out-of-range values fail safely
with sanitized provider errors. Unrelated EXIF is discarded and never logged.

This is source-metadata privacy, not complete anonymity: filenames and collection
names can contain descriptive information. Enabling it deliberately exposes
approved sensitive fields publicly and enables exact source originals, whose bytes may contain
arbitrary additional embedded metadata. No EXIF stripping, re-encoding or remuxing
occurs. Absence of provider coordinates does not establish original-byte safety.

Preview/poster bytes and authorization are unchanged in both modes. Non-null
capabilities advertise implemented representations, not existence guarantees or
authorization grants; projection makes no representation probes. Every enabled
delivery rechecks current lifecycle and policy. RAW/HEIC originals need not be
browser-displayable. Startup configuration is immutable and restart invalidates
cursor signing keys, so privacy mode is not caller-controlled cursor state.

Provider absolute/NAS paths, URLs, credentials, owner/library IDs, checksums, people/albums, visibility flags and raw EXIF/provider JSON never cross the consumer boundary. Full upstream JSON, including ignored EXIF, remains bounded to 1 MiB.

## Failures and consumer responsibilities

All responses use `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`. JSON is buffered before success headers. Invalid/private/missing/unsupported/trashed/offline detail uses fixed 404 `not found`; provider/auth/transport/malformed-response/timeout failures use fixed 502 `media unavailable`. No provider bodies, paths, cursors, coordinates or raw query values enter routine logs.

Consumers own picker/tree UI, selected-reference persistence, gallery ordering/captions, derivatives and presentation of publicly available coordinates. Media Gateway owns current eligibility and safe metadata; it has no catalog database, media manager or web UI.
