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

## Get started / operator guide

1. [Configure a first instance](configuration.md#get-started): prerequisites,
   dedicated credential and a minimal single-root configuration.
2. [Understand media behavior](architecture.md#preview-delivery): previews,
   originals, video Range/HEAD/206/416, revocation and streaming limits.
3. [Install and deploy](deployment.md): dedicated service identity, protected
   files, loopback listener and nginx isolation.
4. [Validate and operate](release.md): checksummed bundle, smoke checks,
   install/upgrade/rollback and final disposable qualification.

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

## Technical reference and project state

- [Architecture and media behavior](architecture.md)
- [Configuration and Policy v2](configuration.md)
- [Security model](security.md)
- [Toolchain and local validation](toolchain.md)
- [Product-completion contract](product-completion.md)
- [Roadmap and accepted milestones](roadmap.md)

#38–#41 are accepted through `7053ac1f297b97be6daacca7ff450b442cb689e7`.
#42 is the final #37 child: final bundle qualification and post-merge Pages
verification remain acceptance gates. Production hostname/Cloudflare cutover is
separate in `stef-k/server-migration#10`; #30 is optional derivative work.
