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
private archive -> Immich -> Media Gateway (loopback) -> nginx -> public catalog + media
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
`privacy.expose_source_metadata=true` deliberately to enable sensitive public catalog
fields and exact originals, whose bytes may contain embedded metadata. Filenames
and collection names remain visible in both modes.

## Operator / administrator

Install and administer the gateway on the host that can reach private Immich:

1. [Download and verify a release](release.md#download-and-verify-a-release).
2. [Configure and run a first instance](configuration.md#get-started): provision
   the credential, set publication rules and test in the foreground.
3. [Install systemd and nginx](deployment.md#operator-path): follow the practical
   host setup sequence to expose only public catalog and media routes.
4. [Validate delivery](deployment.md#validation-checklist), then follow
   [operations, upgrades and rollback](release.md#upgrade-and-rollback).

## Application integrator

A **consumer** is an application or browser that discovers eligible media
and uses it in its own interface. It is not a gallery UI supplied by the gateway.

[Follow the integration walkthrough](catalog-api.md#integration-walkthrough) to
browse collections, page through assets and combine returned media paths with one
public Gateway origin. The [catalog contract](catalog-api.md#routes-and-selectors)
covers nullable metadata, pagination, credential-free CORS and reauthorization.

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
