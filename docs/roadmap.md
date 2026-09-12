# Roadmap

Media Gateway deliberately uses a small roadmap. The phases describe capability boundaries rather than release dates.

Execution tracker: [#1 — V0 tracking](https://github.com/stef-k/media-gateway/issues/1)

## V0 — secure image publication gateway

Goal: prove one narrow end-to-end path from a private Immich asset to a public image response while keeping private media fail-closed.

### #2 — Go service foundation and configuration

- Go module and executable foundation;
- TOML configuration with strict startup validation;
- separate provider credential file;
- loopback-only listener;
- small logging/health behavior;
- test/CI/build baseline.

No provider policy or media delivery yet.

### #3 — Immich provider and publication policy

- current Immich API/permission verification;
- bounded provider client;
- current asset metadata/media type/path lookup;
- allowed-root validation;
- exact `public`, `public-images`, `public-videos` segment policy;
- fail-closed unit/fuzz coverage.

This is the main authorization boundary.

### #4 — public image delivery

- explicit `GET`/`HEAD` image route;
- policy re-evaluated on every request;
- suitable Immich preview/representation streaming;
- bounded provider/public request behavior;
- non-enumerating denial;
- real representative proof that the selected public derivative does not expose sensitive EXIF/GPS.

Do not add originals, video, transcoding or cache unless the evidence requires it.

### #5 — Linux deployment and ingress hardening

- Linux build/install path;
- unprivileged systemd service;
- separate secret/config installation;
- nginx public-route boundary;
- HTTPS/tunnel integration guidance;
- production smoke tests proving known-public success and known-private denial.

Completion of #2–#5 establishes V0.

## V0.1 — consumer/editor integration

### #6 — private consumer API and WordPress contract

Expose only the minimal localhost-only discovery/metadata contract needed by trusted consumers such as `stef-k/divi-child`.

The gateway must return only publication-eligible assets and must never give WordPress the privileged Immich credential or allow consumer state to override publication policy.

Actual WordPress theme integration remains a cross-repository change in `divi-child` once this contract is stable.

## V1 candidates — only with evidence

### Video delivery

Add `public`/`public-videos` video support only after specifying and testing:

- byte ranges;
- seek/stream behavior;
- timeouts and disconnects;
- content type/length semantics;
- edge/cache behavior;
- large-file resource limits.

### Derived image variants/cache

Consider gateway-side resizing/re-encoding/cache only if provider previews do not meet required quality/privacy/performance or if production traffic demonstrates a need.

A cache must never become independent publication authority.

### Signed or opaque URLs

Provider asset IDs are not treated as secrets in V0 because every delivery is policy checked. Add signed/opaque identifiers only if they solve a demonstrated abuse, privacy or integration problem.

### Additional providers

Do not build a dynamic provider framework. Introduce a second provider only when a concrete integration establishes requirements that the small internal provider seam cannot already satisfy.

## Explicit non-goals

Unless a later roadmap revision establishes evidence, Media Gateway does not become:

- a photo/video manager;
- a gallery site;
- a WordPress plugin;
- a generic proxy or URL fetcher;
- a NAS browser;
- an asset/publication database;
- an authentication/identity platform;
- a replacement for Immich.
