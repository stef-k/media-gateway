---
title: "Product model and scope"
---

# Product model and scope

Media Gateway is a small, fail-closed publication boundary between private Immich
media and public HTTP delivery. Directory conventions determine eligibility;
consumer selections never grant publication authority.

## Publication by convention

Operators configure named, non-overlapping provider roots and exact directory
components such as `public`, `public-images`, `public-videos` or a root-scoped
`post`. Rules combine with OR semantics and can apply globally or to selected
logical roots. Provider paths are metadata, never local mounts.

An archive can keep its existing year/trip/device organization:

```text
archive/
  2025/trip/public-images/photo.jpg
  2025/trip/public-videos/clip.mp4
  2025/trip/private/photo.jpg
```

With matching configured conventions, the first two assets are eligible and the
third remains private. No negative rule or per-asset publication database is
needed. Moving media outside a matching convention revokes future requests once
Immich reports the new path or lifecycle state. Revocation cannot recall bytes
already received or stop an already-authorized response.

See [configuration](configuration.md) for the complete Policy v2 schema, examples
and migration from obsolete settings.

## Discovery and delivery

Trusted same-host applications browse paginated collections and image/video
assets through the loopback catalogue. They receive logical root names,
root-relative collection paths, filenames, validated nullable metadata and stable
gateway paths. Provider absolute paths, credentials and raw EXIF stay private.
Search filters narrow candidates; every candidate still passes current policy.

A consumer may store an asset ID or gateway URL, but each public delivery request
independently rechecks current provider lifecycle and publication policy:

- `GET/HEAD /media/<id>/preview` returns a fixed provider-generated image preview
  or video poster.
- `GET/HEAD /media/<id>/original` returns authorized provider source bytes,
  including embedded EXIF/GPS. No conversion or metadata stripping is implied.
- Video originals support validated single byte ranges and 206/416 responses.
  Image originals and previews ignore Range; conditional requests are unsupported.
- Original streams use 60-second read/write inactivity bounds. Metadata,
  connection/header work and shutdown remain separately bounded.

The [consumer guide](consumer-api.md) defines pagination and projection;
[architecture](architecture.md#preview-delivery) defines media framing, provider
failure handling and streaming behavior. Original source formats such as RAW/HEIC
need not be browser-displayable; consumers own their downstream transformations.

## Deployment boundary

One Go binary uses strict startup TOML and a separately protected credential.
The application listens only on numeric loopback. nginx publishes the explicit
media routes and denies the trusted `/internal/` surface. Immich remains private;
there is no direct NAS access, caller-selected upstream or storage fallback.

The reference deployment uses Linux amd64, an unprivileged systemd service,
nginx access/error logs and sanitized application diagnostics in journald.
See [security](security.md), [deployment](deployment.md) and the
[bundle, smoke and rollback guide](release.md). Validate provider behavior on the
intended host; a successful build alone is not deployment qualification.

## Scope limits

Media Gateway does not provide a gallery/editor, media manager, CMS integration,
publication database, NAS browser, generic URL proxy, public catalogue or identity
platform. It does not offer arbitrary resize/crop/quality transformations,
transcoding, HLS/DASH, a cache or a metrics stack.

Additional fixed browser-safe derivatives are optional future scope and would
remain distinct from original source delivery. See the [roadmap](roadmap.md) for
other evidence-driven directions.
