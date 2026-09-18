---
title: "Roadmap"
---

# Roadmap

Media Gateway focuses on convention-driven publication of private provider-managed
images and videos. The [product model](product-completion.md) defines its scope;
optional directions below are not promises or scheduled releases.

## Current capabilities

- Named provider roots with global and root-scoped exact directory conventions.
- Fail-closed startup, current lifecycle/policy checks and non-enumerating denials.
- Paginated loopback image/video collections, safe metadata and stable media paths.
- Fixed image previews and video posters, plus exact source originals.
- Video single byte ranges, GET/HEAD framing and inactivity-bounded original streams.
- Private Immich integration using a dedicated least-privilege credential.
- Linux amd64 bundles with checksums, offline guides, portable smoke checks and
  install/upgrade/rollback procedures for the systemd/nginx reference deployment.

See [configuration](configuration.md), the [consumer guide](consumer-api.md) and
[media behavior](architecture.md#preview-delivery) for the supported contracts.
Immich 3.2.0 is the deployed validation baseline; provider upgrades require
renewed representative checks.

Source-metadata exposure is **off by default**. Capture/local timestamps,
coordinates and original paths remain present as null; originals return fixed 404
without provider fetches. Qualified previews/posters remain available. Set
`privacy.expose_source_metadata=true` deliberately to enable sensitive catalogue
fields and exact originals, whose bytes may contain embedded metadata. Filenames
and collection names remain visible in both modes.

## Optional future directions

The settled surface is catalogue, preview/poster and privacy-permitted exact
original. Consumers own browsing, selection, presentation and their derivative or
caching strategy. Additional gateway representations are not standing future work;
a measured new need requires a fresh bounded issue.

### Signed or opaque URLs

Provider asset IDs are not secrets because delivery reauthorizes every request.
Signed or opaque public identifiers require a demonstrated abuse, privacy or
integration need.

### Additional providers

Another concrete integration may justify extending the small provider boundary.
A generic provider framework is not needed for the current Immich integration.

### Caching and transformations

Gateway caching, arbitrary resize/crop/quality APIs and transcoding are outside the
current product. Any proposal needs measured requirements and must preserve fresh
publication authorization; cached bytes cannot become independent authority.

## Distribution and operations

Immutable versioned binaries are distributed through
[GitHub Releases](https://github.com/stef-k/media-gateway/releases). The
[bundle verification and operations guide](release.md) describes download,
checksum verification and installation. Host-specific public routing remains an operator
deployment decision after validation.
