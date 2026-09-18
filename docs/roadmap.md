# Roadmap

Media Gateway deliberately uses a small roadmap. Phases describe capability boundaries rather than release dates.

Historical V0 tracker: [#1 — V0 tracking](https://github.com/stef-k/media-gateway/issues/1)

Current product-completion epic: [#37 — convention-driven media publication product](https://github.com/stef-k/media-gateway/issues/37)

Toolchain authority: [docs/toolchain.md](toolchain.md)

Logging authority: [docs/logging.md](logging.md)

Target product contract: [docs/product-completion.md](product-completion.md)

## Accepted V0 — secure image-preview publication foundation

V0 is complete at `d3948b7e2e9de9135fbd353a10b371fb727e9be3`.

Its purpose was to prove the security/deployment boundary with one narrow real-provider representation before broadening the media plane.

### #2 (accepted) — Go service foundation and configuration

- Go 1.27 family with Go 1.27.1 reviewed baseline;
- strict TOML configuration and separate provider credential;
- fail-closed startup validation;
- standard-library service shell/lifecycle/logging;
- loopback-only listener and lightweight CI.

### #3 (accepted) — Immich provider and publication policy

- fixed trusted Immich authority;
- bounded metadata client;
- current asset media/path/lifecycle lookup;
- canonical root/path checks;
- exact literal publication-segment authorization;
- fail-closed tests and sanitized errors/logging.

The accepted policy already understands image/video media types, but V0 delivery/catalogue intentionally implemented images first.

### #4 (accepted) — public image preview delivery

- exact `GET`/`HEAD /media/<id>/preview`;
- current lifecycle and policy re-evaluated per request;
- bounded JPEG/WebP preview relay;
- no redirects/original fallback;
- non-enumerating denials;
- real Immich 3.2.0 phone/camera/RAW privacy, visual quality and lifecycle-revocation qualification.

`/preview` is an accepted representation, not the final definition of a published asset or a permanent maximum-quality commitment.

### #5 (accepted) — Linux deployment and ingress hardening

- unprivileged systemd service and protected config/key layout;
- nginx-only public boundary;
- route/Host/method isolation;
- sanitized nginx vs journald logging split;
- Linux amd64 bundle/checksums;
- portable smoke validation;
- deterministic install/upgrade/rollback;
- real M6 systemd/nginx/bundle/outage/rollback qualification.

The real public `media.stefk.me`/Cloudflare cutover remains owned by `stef-k/server-migration#10` and should wait for #37 product completion.

## Accepted trusted consumer foundation

### #6 (accepted) — private eligible-image consumer API

#20/#21 provide loopback-only bounded browse/detail for eligible images and #26 adds validated nullable coordinates behind the historical V0 feature flag.

This proved the consumer seam but does not define the final media catalogue. Historical limitations corrected by the #37 lane include image-only search/projection, preview-only stable paths, provider-candidate pagination semantics and optional coordinates.

## Product completion — original convention-driven media gateway

The original requirement is a secure **media** publication gateway where configurable filesystem conventions are the publication source of truth and trusted consumers can browse/select eligible image/video media without gaining provider/storage authority.

Deterministic sequence:

```text
#38 -> #39 -> #40 -> #41 -> #42 -> close #37
```

### #38 — accepted policy v2: named roots and scoped conventions

Replaced the flat `allowed_roots` list with named roots and keep rules as OR-ed exact directory conventions that may be global or scoped to selected logical roots.

Accepted example:

```toml
[[policy.roots]]
name = "images"
path = "/media/archive/Images"

[[policy.roots]]
name = "art"
path = "/media/archive/ART"

[[policy.rules]]
segment = "public"
media = ["image", "video"]

[[policy.rules]]
segment = "public-images"
media = ["image"]

[[policy.rules]]
segment = "public-videos"
media = ["video"]

[[policy.rules]]
segment = "post"
media = ["image"]
roots = ["images"]
```

Root paths are provider-visible POSIX metadata paths, never NAS mounts. Root names are stable logical identifiers. Omitted rule roots mean global; explicit root lists cage a convention. Overlapping roots are rejected.

### #39 — accepted paginated generic eligible-media catalogue

The implementation generalizes the trusted consumer surface to images and videos organized as safe logical collections.

Catalogue contract:

```text
GET /internal/collections?limit=<n>&cursor=<opaque>
GET /internal/assets?root=<name>&collection=<relative-path>&limit=<n>&cursor=<opaque>
GET /internal/assets/<id>
```

Requirements:

- collection and asset browsing always paginated;
- small default page size, hard maximum 100;
- gateway-signed bounded continuation, invalidated on restart;
- bounded internal provider scanning/filling;
- provider search may optimize discovery but never authorize;
- logical root/relative collection/filename exposed, absolute provider path hidden;
- image/video media type, nullable dimensions and `duration_ms`, times, always-present nullable coordinates, and non-null representation capabilities;
- image/video preview/original paths are implemented without representation probes;
- at-least-once collections with in-page deduplication, no counts or folder view;
- coordinates become normal validated trusted metadata rather than an operator feature flag;
- no consumer selector/reference grants publication.

### #40 — accepted image originals

Implemented:

```text
GET/HEAD /media/<id>/original
```

For images, original means exact provider original bytes after current lifecycle/policy reauthorization. No silent preview/full fallback, RAW/HEIC/JPEG conversion or metadata stripping. Consumers own resizing/derivatives.

#40 is accepted on main through `d57e6a955af190df719e5fe0be444442630b8125`. Fixed GET original uses no query and `asset.download`; stale `[delivery]` fails migration. #41 extends the original transport lifetime. #42 still owns final product/public-host reconciliation.

### #41 — accepted video, ranges and long streaming

Implemented in the existing catalogue/provider/delivery seams:

- generic catalogue inclusion and video metadata;
- fixed video preview/poster with real-provider privacy qualification;
- original video bytes;
- correct practical byte-range semantics (`Range`, `206`, `Content-Range`, `Accept-Ranges`, 416/HEAD behavior);
- long media streams no longer limited by V0 preview-only absolute request/write lifetime;
- authorization/connect/header phases remain bounded;
- established streams use bounded inactivity/client-disconnect/shutdown semantics;
- nginx forwards only explicitly supported media headers and never provider/storage authority.

No gateway transcoding/HLS/DASH is included. PR #47 merged as `7053ac1f297b97be6daacca7ff450b442cb689e7` after exact-head M6 Immich 3.2.0 poster/original-range, byte identity, >65-second active stream, revocation/ingress and mandatory cleanup passed. No persistent M6 installation or public cutover was performed.

### #42 — final product docs, bundle and qualification

#38–#41 are accepted. #42 is the final open child of #37:

- reconcile all docs/config examples with actual behavior;
- ship a final named-root/global-and-scoped TOML example;
- document paginated collection/asset JSON for image/video;
- document preview vs original metadata/privacy semantics;
- extend bundle/smoke validation for original image/video and representative ranges;
- after software/docs review, qualify the exact unmerged head/retained CI bundle on disposable M6 infrastructure and clean up;
- after merge, verify shared-theme Pages rendering and links under `/media-gateway`;
- close #37 and unblock the host-specific production cutover.

## Later optional product evolution

### #30 — additional fixed safe image representations

#30 may add explicit thumbnail/normal/large browser-safe provider derivatives if concrete consumer/performance evidence justifies them.

#30 is **not** a substitute for `/original` and does not block #37 unless a concrete implementation dependency is discovered.

### Signed or opaque URLs

Provider asset IDs are not secrets because delivery reauthorizes every request. Add signed/opaque public identifiers only for a demonstrated abuse/privacy/integration requirement.

### Additional providers

Do not build a generic provider framework. Introduce another provider only when a concrete integration establishes requirements that the small internal seam cannot support.

### Cache / transformations

Do not add gateway cache, arbitrary resize/crop/quality APIs or transcoding merely because they are common media-server features. A cache must never become independent publication authority.

## Explicit non-goals

Unless later evidence creates a bounded issue, Media Gateway does not become:

- a photo/video manager;
- a gallery/editor site;
- a WordPress/Wayfarer plugin;
- a NAS browser or direct filesystem server;
- a publication/selection database;
- a generic URL proxy;
- an authentication/identity platform;
- an image CDN/transformation service;
- a replacement for Immich.
