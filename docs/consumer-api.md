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

Example response:

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
and returns one object with the same six fields shown above. Missing, private,
malformed-path, unsupported, trashed/offline and invalid-ID results share `404` with body
`not found\n`; knowledge of a private ID grants no information.

Dimensions are nonnegative integers or `null` when unknown. Times retain the
adapter's validated RFC3339 strings, including fractional precision;
`file_created_at` is capture time and `local_date_time` preserves provider local
wall-clock meaning without timezone conversion. The relative `preview_path` is
constructed only from a validated ID and the fixed public preview route.

No provider/NAS paths, URLs, credentials, filenames, checksums, owner/library IDs,
EXIF/GPS or raw provider JSON are serialized. GPS/EXIF and richer search filters
are deferred until a concrete WordPress requirement establishes a minimum safe
contract. WordPress implementation belongs in its own repository.

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
