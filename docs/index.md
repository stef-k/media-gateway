---
title: Media Gateway
permalink: /
---

# Media Gateway

Publish selected images and videos from private Immich through a small,
fail-closed gateway. Configure named provider roots and exact directory conventions
such as `public`, `public-images`, `public-videos`, or a root-scoped `post`.
Every request rechecks current lifecycle and policy. Moving media out of an eligible
convention revokes future delivery once Immich reports the change.

[View on GitHub / project README](https://github.com/stef-k/media-gateway#readme)

```text
private archive -> Immich -> Media Gateway (loopback) -> nginx -> public media
                                  |
                         trusted same-host catalogue
```

The gateway supplies fixed preview/poster and original URLs for images and videos,
paginated eligible collections, video ranges and long original streams bounded by
inactivity. Originals include source metadata such as EXIF/GPS. Consumer references
never grant publication authority. There is no gallery UI, database, direct NAS
access, arbitrary proxy or transcoding service.

Supported Linux amd64 binaries are distributed through
[GitHub Releases](https://github.com/stef-k/media-gateway/releases).
[Download and verify a release](release.md#download-and-verify-a-release) before
installation; see the [changelog](https://github.com/stef-k/media-gateway/blob/main/CHANGELOG.md)
for operator and consumer changes.

Source-metadata exposure is **off by default**. Capture/local timestamps,
coordinates and original paths remain present as null; originals return fixed 404
without provider fetches. Qualified previews/posters remain available. Set
`privacy.expose_source_metadata=true` deliberately to enable sensitive catalogue
fields and exact originals, whose bytes may contain embedded metadata. Filenames
and collection names remain visible in both modes.

## Get started / operator guide

1. [Configure a first instance](configuration.md#get-started): prerequisites,
   dedicated credential and a minimal single-root configuration.
2. [Understand media behavior](architecture.md#preview-delivery): previews,
   originals, video Range/HEAD/206/416, revocation and streaming limits.
3. [Install and deploy](deployment.md): dedicated service identity, protected
   files, loopback listener and nginx isolation.
4. [Validate and operate](release.md): checksummed bundle, smoke checks,
   install/upgrade/rollback and isolated deployment validation.

## Consumer guide

[Integrate a trusted same-host application](consumer-api.md): browse collections,
page through images/videos, resolve detail and use stable gateway URLs. The guide
covers nullable metadata, cursor restart behavior and publication reauthorization.
Never expose `/internal/` through a public proxy.

## Deployment and operations

- [Release and smoke procedure](release.md)
- [Deployment and reviewed Immich contracts](deployment.md)
- [Logging and privacy](logging.md)
- [Video qualification worksheet](video-qualification.md)

## Technical reference

- [Architecture and media behavior](architecture.md)
- [Configuration and publication policy](configuration.md)
- [Security model](security.md)
- [Toolchain and local validation](toolchain.md)
- [Product model and scope](product-completion.md)
- [Current capabilities and roadmap](roadmap.md)

The supported provider is Immich, validated on version 3.2.0. The reference
platform is Linux amd64 with systemd and nginx.
