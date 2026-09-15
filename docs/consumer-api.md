# Trusted consumer API

> **Current vs target:** the accepted V0 implementation exposes loopback-only image `GET /internal/assets` browse/detail with provider-candidate pagination and optional coordinates. Issue #39 replaces that slice with the product-complete **paginated eligible image/video catalogue** described below. Until #39 is accepted, callers must use the current V0 behavior implemented by `main`.

The consumer API is for trusted same-host applications through the configured numeric loopback listener. It requires a loopback TCP peer and never trusts forwarded address headers. **nginx must never publish `/internal/`**. A localhost reverse proxy still appears local to the application, so the peer check does not replace the nginx route boundary.

Consumer selectors/references never grant publication permission. Every item returned by browse/detail must be currently lifecycle-active and pass authoritative publication policy. Every later public preview/original request independently reauthorizes again.

## Accepted V0 surface

Current `main` provides:

```text
GET /internal/assets?limit=<1..100>&cursor=<opaque>
GET /internal/assets/<asset-id>
```

It searches images only, defaults to 25 provider candidates, and may return fewer/zero eligible assets plus a continuation cursor because policy filtering occurs after one provider candidate page. Its safe object contains ID, dimensions, capture/local time, preview path, and optionally validated latitude/longitude when the current config flag is enabled.

The current contract never returns provider/NAS paths, URLs, credentials, filenames, checksums, owner/library IDs or raw EXIF.

Those V0 properties remain useful security evidence, but image-only/provider-page semantics are not the final product contract.

## Target product catalogue (#39)

Consumers need two bounded paginated levels:

```text
GET /internal/collections?limit=<n>&cursor=<opaque>
GET /internal/assets?root=<logical-root>&collection=<relative-path>&limit=<n>&cursor=<opaque>
GET /internal/assets/<asset-id>
```

The exact final encoding/query rules are owned by #39, but the requirements here are authoritative.

### Collection browsing

A collection represents an eligible publication directory as safe logical metadata:

```json
{
  "root": "images",
  "collection_path": "2022/Egypt Oct 2022/public-images"
}
```

`root` is the logical name from #38 configuration. `collection_path` is relative to that root. Neither field exposes the provider's absolute root, NAS path or provider URL.

Only collections containing at least one currently eligible asset may appear. If complete image/video counts cannot be obtained correctly and boundedly, omit them rather than publish misleading partial counts.

Collections themselves are paginated because a large archive may contain many public directories.

### Asset browsing within a collection

Opening one collection returns a bounded page of currently eligible assets. A directory with thousands of files must never overwhelm the consumer.

Pagination requirements:

- small default page size around the existing 25;
- hard maximum 100;
- opaque bounded cursor;
- no unbounded directory dump;
- consumer-visible continuation represents the eligible browse operation;
- provider search/paging/filtering remains internal;
- any internal fill/scan across provider pages has a hard candidate/page/time budget;
- if the work budget is reached before provider exhaustion, return safe continuation instead of scanning indefinitely.

Provider-side path/root filtering may optimize discovery after current Immich API verification, but it never authorizes an asset.

### Safe asset object

Target image example:

```json
{
  "id": "12345678-1234-4234-8234-123456789abc",
  "media_type": "image",
  "root": "images",
  "collection_path": "2019/Romania Dec 2019/post",
  "filename": "DSC01234.JPG",
  "width": 6000,
  "height": 4000,
  "duration": null,
  "file_created_at": "2019-12-08T10:21:00Z",
  "local_date_time": "2019-12-08T12:21:00Z",
  "latitude": 44.4268,
  "longitude": 26.1025,
  "preview_path": "/media/12345678-1234-4234-8234-123456789abc/preview",
  "original_path": "/media/12345678-1234-4234-8234-123456789abc/original"
}
```

Target video example:

```json
{
  "id": "12345678-1234-4234-8234-123456789abc",
  "media_type": "video",
  "root": "images",
  "collection_path": "2022/Egypt Oct 2022/public-videos",
  "filename": "DJI_0042.MP4",
  "width": 3840,
  "height": 2160,
  "duration": 23.8,
  "file_created_at": "2022-10-10T08:00:00Z",
  "local_date_time": "2022-10-10T10:00:00Z",
  "latitude": null,
  "longitude": null,
  "preview_path": "/media/12345678-1234-4234-8234-123456789abc/preview",
  "original_path": "/media/12345678-1234-4234-8234-123456789abc/original"
}
```

Exact field encodings/nullability must be fixed by #39 tests/provider verification, but the allowlist must not grow into raw provider metadata.

Never serialize:

- absolute original/provider/NAS path;
- provider URL or API key;
- provider library/owner identifiers;
- people/albums;
- raw EXIF/provider JSON;
- arbitrary provider fields.

## Coordinates

Product completion treats coordinates as normal trusted catalogue metadata rather than an optional consumer feature.

Continue the accepted safety semantics:

- decode only latitude and longitude from the provider metadata needed for that pair;
- both unknown -> nullable pair;
- both valid numbers -> expose after eligibility;
- one missing/null while the other is numeric, nonnumeric/non-finite values, latitude outside `[-90,90]` or longitude outside `[-180,180]` -> provider metadata failure;
- zero and inclusive range boundaries are valid;
- no altitude, accuracy, camera/EXIF details or geocoding;
- never log coordinates.

#39 owns removal/migration of the current `[consumer].expose_coordinates` config flag. Do not remove it ahead of the code migration.

## Selectors are never authorization

A request such as:

```text
root=images&collection=2019/Romania Dec 2019/raws
```

must not expose that private directory merely because the consumer named it.

For every candidate, Media Gateway must resolve current provider metadata/lifecycle and require the #38 policy result. Root/collection/cursor/asset ID are selectors only.

Exact detail lookup must independently reauthorize. A previously returned/stored asset reference may later become a 404 after the file is moved out of a publication convention or becomes lifecycle-unavailable.

## Preview and original references

The target catalogue advertises stable gateway routes, never provider URLs:

```text
/media/<id>/preview
/media/<id>/original
```

Preview is used for picker/grid/poster UX. Original is the authorized provider-original media route owned by #40/#41. A catalogue reference never guarantees future access; delivery reauthorizes current state.

## Bounds and failures

Keep the accepted principles:

- `Cache-Control: no-store` and `X-Content-Type-Options: nosniff` on trusted JSON;
- bounded query/cursor/output/provider JSON sizes;
- fixed sanitized errors;
- no provider error bodies or private values in output/logs;
- client disconnect cancels work;
- no unbounded automatic provider paging;
- no database/cache merely to support browse pagination.

#39 will set exact collection-page/asset-page response-size and internal scan budgets from evidence.

## Consumer responsibilities

Consumers own:

- folder/tree/picker UI;
- selected asset persistence;
- gallery/order/caption state;
- downstream resizing/derivative generation if desired;
- publication of coordinates through their own UX/privacy decisions.

Media Gateway owns current eligibility, safe browse metadata and stable policy-checked media references. It does not become a CMS or gallery database.
