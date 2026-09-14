# Private eligible image consumer API

This API is for trusted same-host consumers through the configured numeric
loopback listener. It requires a loopback TCP peer and does not trust forwarded
address headers. **nginx must never publish `/internal/`**: publish only `/media/`
and deny every other route. A localhost reverse proxy still appears local to the
application; the peer check does not replace the nginx route boundary. No CORS
permission or consumer authentication system is added.

The only consumer routes are:

```text
GET /internal/assets
GET /internal/assets/<asset-id>
```

Other methods, route variants, escaped paths and invalid IDs receive the fixed
`404` denial. IDs must match the accepted Immich UUIDv4 form. No path cleaning or
redirect occurs. Request bodies are not consumed or forwarded.

## Browse

`GET /internal/assets?limit=25&cursor=<opaque-value>` accepts only these optional
query keys, each at most once and with a nonempty value:

- `limit`: decimal integer 1–100; default 25. It bounds **provider candidates**,
  not the number of eligible images promised in a page.
- `cursor`: the previous response's `next_cursor`, URL-encoded as a query value.
  Treat it as opaque continuation state, never as metadata or authorization.
  It is limited to 1024 UTF-8 bytes and cannot contain Unicode control characters.

The complete encoded query is limited to 4096 bytes. Unknown keys, malformed
encoding, invalid limits/cursors and duplicate values fail with `404` before
provider work. There are no caller-selected provider filters, paths, URLs, media
types or ordering controls. A cursor is passed only in the fixed search body's
cursor field; it cannot change gateway-owned filters or the configured origin.

One request performs one `SearchCandidates` call, with the adapter's fixed
image-only, newest-capture-first ordering. The adapter requires explicit active
lifecycle booleans and omits trashed/offline records before returning candidates,
while preserving the provider cursor. Malformed lifecycle fields reject the
whole page with the existing sanitized 502. Every returned candidate must then
pass `publication.Eligible` using its unchanged provider path and media type.
Private, outside-root, near-miss and malformed paths are silently omitted.

Default/disabled response (`consumer.expose_coordinates=false`):

```json
{
  "assets": [
    {
      "id": "12345678-1234-4234-8234-123456789abc",
      "width": 640,
      "height": null,
      "file_created_at": "2026-09-13T10:00:00Z",
      "local_date_time": "2026-09-13T12:00:00Z",
      "preview_path": "/media/12345678-1234-4234-8234-123456789abc/preview"
    }
  ],
  "next_cursor": "opaque-continuation"
}
```

`assets` is always an array, including `[]`. `next_cursor` is omitted when
pagination ends. An empty eligible page can still have a continuation cursor:
consumers must not infer completion from the item count. Pagination is not a
snapshot of a changing provider library.

## Detail and safe fields

`GET /internal/assets/<asset-id>` accepts no query parameters. It performs a fresh
exact candidate lookup, requires image media and current publication eligibility,
and returns one object with the same fields as browse (six when coordinates are
disabled). Missing, private,
malformed-path, unsupported, trashed/offline and invalid-ID results share `404` with body
`not found\n`; knowledge of a private ID grants no information.

Dimensions are nonnegative integers or `null` when unknown. Times retain the
adapter's validated RFC3339 strings, including fractional precision;
`file_created_at` is capture time and `local_date_time` preserves provider local
wall-clock meaning without timezone conversion. The relative `preview_path` is
constructed only from a validated ID and the fixed public preview route.

No provider/NAS paths, URLs, credentials, filenames, checksums, owner/library IDs,
raw EXIF or raw provider JSON are serialized. Richer search filters remain outside
this contract.

## Opt-in coordinates

`[consumer].expose_coordinates` defaults to false when the section or flag is
omitted. Disabled mode requests `withExif=false`, ignores EXIF even if supplied
by the provider, and omits latitude/longitude keys entirely. The six-field JSON
contract above is unchanged.

When true, the same fixed structured search requests `withExif=true`. Only
`exifInfo.latitude` and `exifInfo.longitude` are decoded from EXIF. Every active
candidate is validated, then authorized with current `publication.Eligible`,
then projected into the consumer allowlist. Private/outside-root/near-match and
malformed paths expose nothing. Lifecycle-unavailable candidates are omitted
before coordinate decoding, preserving pagination and exact-detail 404 behavior.

Enabled detail example (browse wraps the same object in `assets`):

```json
{
  "id": "12345678-1234-4234-8234-123456789abc",
  "width": 640,
  "height": null,
  "file_created_at": "2026-09-13T10:00:00Z",
  "local_date_time": "2026-09-13T12:00:00Z",
  "preview_path": "/media/12345678-1234-4234-8234-123456789abc/preview",
  "latitude": 25.0584,
  "longitude": 121.635
}
```

Both coordinate fields absent (including absent/null `exifInfo`), or both fields
present and `null`, produce `"latitude":null,"longitude":null` on an otherwise
eligible object. Exactly one field present, even if `null`, or a numeric value
paired with null is invalid. A nonnumeric/non-finite value, latitude outside
`[-90,90]`, or longitude outside `[-180,180]` rejects the provider page through the fixed sanitized 502
path, with no partial results. Zero and the inclusive boundary values are valid.
No altitude, accuracy, camera details, people/albums or other EXIF is exposed.
Coordinates and unrelated EXIF never enter application logs.

This deliberately supports concrete consumer features: `stef-k/divi-child` stores
attachment coordinates for public gallery Maps links and Wikipedia geosearch;
`stef-k/Wayfarer` uses coordinates for maps, Maps links and Wikipedia geosearch
after its own trip/timeline publication and privacy checks. Those applications
own their public UX and integration code; this gateway has no coupling to either.
There is no per-asset override, geocoding, database or coordinate cache.

Public `/media/<id>/preview` GET/HEAD routes, headers and bytes are unchanged.
GPS is deliberately available only on the trusted eligible-consumer metadata
plane; preview bytes remain metadata-minimal and no public metadata route exists.

## Bounds, failures and authorization lifetime

Responses use `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
Successful responses are `application/json`, with a maximum encoded size of
64 KiB and at most 100 assets. Oversized output fails before success headers.
The existing adapter bounds provider JSON to 1 MiB and rejects malformed provider
responses. Exceptional provider/validation failures produce fixed `502` text
`media unavailable\n`, with no provider details. This includes HTTP 404 from
the provider search endpoint; a genuinely missing exact candidate is a successful
empty search page and remains a consumer 404. Routine denials and successful
requests remain quiet; exceptional provider failures use the existing sanitized
logging classes. Canceled requests do not generate warnings.

The existing provider timeout bounds each search through body reading; a
60-second request context also caps consumer work. Client disconnects cancel it.
Existing finite HTTP header/read/write bounds apply. There are no automatic page
scans, retries or caches.

Results establish eligibility only at browse/detail time. Consumer references
never grant publication: `/media/<id>/preview` independently fetches current
metadata, requires current lifecycle availability and reauthorizes on every GET/HEAD.

Issue #21 supplies software evidence only. Real-Immich preview privacy/quality
qualification in #18/#4 and host/nginx qualification in #5 remain independent.
