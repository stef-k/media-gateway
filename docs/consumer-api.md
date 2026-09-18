---
title: "Trusted consumer API"
---

# Trusted consumer API

The trusted catalogue serves trusted same-host applications through the numeric loopback listener. The TCP peer must be loopback; forwarded identity headers are ignored and no CORS grant is provided. **nginx must never publish `/internal/`**. A localhost proxy appears local, so the peer check does not replace ingress isolation.

## Integration walkthrough

Call the numeric loopback gateway directly from the trusted application, never
through the public vhost. The following uses synthetic IDs and logical selectors:

```sh
# Browse a small page; URL-encode each subsequent opaque cursor unchanged.
curl --fail --get 'http://127.0.0.1:2290/internal/collections' \
  --data-urlencode 'limit=25'
```

Select one returned collection; descendants are separate collections. Use the query
key `collection` (the JSON response calls the field `collection_path`):

```sh
curl --fail --get 'http://127.0.0.1:2290/internal/assets' \
  --data-urlencode 'root=photos' --data-urlencode 'collection=2026/example-trip/public' \
  --data-urlencode 'limit=25'
curl --fail 'http://127.0.0.1:2290/internal/assets/12345678-1234-4234-8234-123456789abc'
```

Consume each page, then repeat the same operation/selectors with
`--data-urlencode "cursor=$NEXT_CURSOR"` until `next_cursor` is null. A short or
empty page can still have continuation. Restart browsing after gateway restart
invalidates a cursor; merge collections by `(root, collection_path)` and avoid
assuming a provider snapshot. Never print real catalogue responses into public logs.

Use the returned preview/original paths with your configured public gateway origin
for public media, or loopback for trusted local use. Do not infer authorization
from a stored URL. Handle 404 revocation and sanitized 502 failures at delivery.
Your application owns selection persistence, captions and presentation.

## Routes and selectors

Only GET is accepted:

```text
GET /internal/collections?limit=<n>&cursor=<opaque>
GET /internal/assets?root=<logical-root>&collection=<relative-path>&limit=<n>&cursor=<opaque>
GET /internal/assets/<UUIDv4>
```

Detail accepts no query, including an empty `?`. Routes are exact, with no path cleaning or redirects. Unknown/duplicate query keys, explicit empty values, invalid encoding and malformed selectors receive the fixed 404 denial before provider access.

Asset browsing requires an exact configured logical `root` and a canonical relative POSIX `collection`: nonempty valid UTF-8, no leading/trailing slash, empty/dot/dot-dot components, backslashes or control characters. Normal query decoding applies once; selectors are never cleaned or Unicode-normalized into validity. Consumers never supply an absolute provider path.

Every active candidate passes `publication.Evaluate`. Asset browsing additionally requires exact root and parent-collection equality: descendants are separate collections. A valid known-root selector with no eligible media returns 200 with an empty page, without revealing whether a private directory exists. A stored asset reference grants no authority; detail independently rechecks current lifecycle and Policy v2.

## Bounds and pagination

| Boundary | Limit |
| --- | --- |
| Default consumer page | 25 |
| Maximum consumer page | 100 |
| Raw query | 8 KiB |
| Encoded gateway cursor | 2 KiB |
| Decoded collection selector | 2 KiB UTF-8 |
| Provider calls per request | 8 |
| Overall catalogue work | 30 seconds |
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

Collection identity is exactly `(root, collection_path)`, derived only from the successful Policy v2 result for an active eligible asset. No collection counts are returned. `next_cursor` is always present as a string or `null`.

Discovery combines applicable global and root-scoped rules by exact segment, unions media sets, and traverses streams sorted by logical root name then segment. Provider root-prefix and segment-shaped filters only narrow candidates. Immich's case/accent-insensitive matching never replaces exact policy evaluation. Discovery uses `withExif=false` and decodes only lifecycle/path/media facts; unrelated dimensions, times or EXIF cannot fail discovery.

Collection discovery is **at least once**: identities are deduplicated within a response page, but may recur on later pages. Consumers must idempotently merge by `(root, collection_path)`. Stable accessible candidate streams eventually expose their eligible collections. Immich offset pagination is not a snapshot; library mutations can also cause repeats/skips. There are no persistent seen sets or cursor sessions. Immich folder-view endpoints are not used: their unpaginated, timeline-specific behavior does not satisfy this contract.

## Asset JSON

Asset lists return `{"assets": [...], "next_cursor": null}`; detail returns one asset directly. Every asset has exactly these fields (values below are synthetic):

```json
{
  "id": "12345678-1234-4234-8234-123456789abc",
  "media_type": "image",
  "root": "photos",
  "collection_path": "2025/legacy-trip/post",
  "filename": "DSC01234.JPG",
  "width": 6000,
  "height": 4000,
  "duration_ms": null,
  "file_created_at": "2025-01-15T10:21:00Z",
  "local_date_time": "2025-01-15T12:21:00Z",
  "latitude": 12.3456,
  "longitude": 23.4567,
  "preview_path": "/media/12345678-1234-4234-8234-123456789abc/preview",
  "original_path": "/media/12345678-1234-4234-8234-123456789abc/original"
}
```

`media_type` is `image` or `video`. `filename` is the basename of the validated provider path, never an arbitrary provider display filename. Width, height and `duration_ms` are explicit nullable nonnegative safe JSON integers (maximum 9007199254740991). Duration is **milliseconds**, so a 23.8-second video has `duration_ms: 23800`. Zero is valid; null means unknown. Both required time strings are validated as RFC3339 with optional fractional seconds and preserved; local wall time is not converted to another timezone.

An example video page (synthetic values) uses the same schema. A detail request
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

### Current representation capabilities

| Media | `preview_path` | `original_path` |
| --- | --- | --- |
| image | `/media/<id>/preview` | `/media/<id>/original` |
| video | `/media/<id>/preview` | `/media/<id>/original` |

All capability fields are present and non-null for eligible images and videos. They advertise implemented representations, not existence guarantees or authorization grants. Projection makes no representation probes. A derivative can disappear; every subsequent delivery independently re-fetches current metadata/lifecycle and re-evaluates policy. The metadata schema and millisecond duration units are unchanged.

`/preview` is a separately qualified provider-generated web representation. `/original` returns exact authorized source bytes, including embedded EXIF/GPS, without conversion or metadata stripping; RAW/HEIC need not be browser-displayable. Both require fresh public authorization even after catalogue selection.

### Coordinates and privacy

Latitude and longitude are always present, either both valid numbers or both null. Asset list/detail searches use `withExif=true`; only the coordinate pair is retained. Both absent or both null mean unknown. One absent/present mismatch, one null/numeric mismatch, nonnumeric/non-finite values, latitude outside `[-90,90]` or longitude outside `[-180,180]` fail as provider metadata errors. Zero and inclusive boundaries are valid. Unrelated EXIF is discarded and coordinates are never logged.

There is no `[consumer]` configuration section. Stale `consumer.expose_coordinates` fails startup with migration guidance; remove it and restart. Other unknown configuration remains invalid.

Provider absolute/NAS paths, URLs, credentials, owner/library IDs, checksums, people/albums, visibility flags and raw EXIF/provider JSON never cross the consumer boundary. Full upstream JSON, including ignored EXIF, remains bounded to 1 MiB.

## Failures and consumer responsibilities

All responses use `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`. JSON is buffered before success headers. Invalid/private/missing/unsupported/trashed/offline detail uses fixed 404 `not found`; provider/auth/transport/malformed-response/timeout failures use fixed 502 `media unavailable`. No provider bodies, paths, cursors, coordinates or raw query values enter routine logs.

Consumers own picker/tree UI, selected-reference persistence, gallery ordering/captions, derivatives and any downstream publication of coordinates. Media Gateway owns current eligibility and safe metadata; it has no catalogue database, media manager or web UI.
