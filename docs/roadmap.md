# Roadmap

Media Gateway deliberately uses a small roadmap. The phases describe capability boundaries rather than release dates.

Execution tracker: [#1 — V0 tracking](https://github.com/stef-k/media-gateway/issues/1)

Toolchain authority: [docs/toolchain.md](toolchain.md)

Logging authority: [docs/logging.md](logging.md)

## V0 — secure image publication gateway

Goal: prove one narrow end-to-end path from a private Immich asset to a public image response while keeping private media fail-closed.

### #2 — Go service foundation and configuration

This epic is executed through two deterministic implementation children:

#### #9 — Go module and strict configuration core

- Go 1.27 family with Go 1.27.1 as the initial reviewed stable toolchain;
- Go module foundation;
- `github.com/pelletier/go-toml/v2` v2.4.3 as the expected single third-party runtime dependency;
- typed TOML configuration with strict unknown-field rejection;
- installation-specific provider URL, roots and literal publication segment names;
- separate provider credential file;
- fail-closed startup/configuration validation;
- focused configuration tests.

No HTTP service shell, provider API client or media delivery belongs in #9.

#### #10 — service shell, lifecycle, logging and CI

- `cmd/media-gateway` executable;
- standard-library `net/http`, `log/slog`, `flag`, `context`, `testing` baseline;
- loopback-only listener;
- deterministic startup, signal handling and graceful shutdown;
- sanitized application logging to stderr/stdout for journald capture;
- nginx remains the later public access-log layer; the application does not duplicate a full access log;
- minimal health/readiness only if operationally useful and privacy-safe;
- version/build metadata;
- lightweight CI running formatting, vet, tests, race tests and build.

No provider policy or media delivery belongs in #10. Do not add a web/router framework, DI container, logging framework, ORM or other runtime framework without a concrete issue-backed need.

Completion of #9 and #10 closes #2.

### #3 — Immich provider and publication policy

- current Immich API/permission verification;
- bounded provider client;
- current asset metadata/media type/path lookup;
- normalized path-aware allowed-root validation;
- exact configured literal directory-segment policy (the motivating deployment uses `public`, `public-images`, `public-videos` but those names are not universal requirements);
- fail-closed unit/fuzz coverage;
- private provider paths/metadata remain out of routine logs.

This is the main authorization boundary. Harden/decompose it after #2 establishes the final package seams; the expected implementation split is a pure publication-policy core plus a small Immich adapter.

### #4 — public image delivery

- explicit `GET`/`HEAD` image route;
- policy re-evaluated on every request;
- suitable Immich preview/representation streaming;
- bounded provider/public request behavior;
- non-enumerating denial;
- real representative proof that the selected public derivative does not expose sensitive EXIF/GPS.

#17 implements the deterministic public preview path with fake-provider tests.
#24 corrects Immich v3 missing/inaccessible metadata classification. #27 adds
the provider lifecycle prerequisite after #18 discovered stale eligible paths
on trashed/offline external-library records. #18 remains the real deployed-Immich
privacy/quality and revocation gate; after #27 is accepted it must repeat the
move/rescan test without restarting the gateway. #4 closes only after that
evidence is accepted, and #5 depends on that completion.

Do not add originals, video, transcoding or cache unless the evidence requires it.

### #5 — Linux deployment and ingress hardening

- Linux build/install path;
- unprivileged systemd service;
- separate secret/config installation;
- fully comment-documented example TOML/nginx/systemd assets;
- nginx public-route boundary;
- nginx access/error logging plus application/journald diagnostic split;
- generic HTTPS/tunnel integration guidance;
- portable smoke tests proving known-public success and known-private denial.

#5 owns the reusable deployment contract only. A concrete host/hostname integration belongs to that deployment's own repository; the motivating M6 deployment is tracked by `stef-k/server-migration#10`.

Completion of #2–#5 establishes V0.

## V0.1 — consumer/editor integration

### #6 — private consumer API and WordPress contract

The [localhost consumer contract](consumer-api.md) supplies bounded eligible-image
browse and detail through #20 candidate search and #21 authorization/HTTP wiring
for trusted consumers such as `stef-k/divi-child`.

The gateway must return only publication-eligible assets and must never give WordPress the privileged Immich credential or allow consumer state to override publication policy.

#26 adds the opt-in `consumer.expose_coordinates` capability, default false.
Disabled mode preserves #20/#21 search and the six-field JSON contract; enabled
mode exposes only validated nullable latitude/longitude after current eligibility.
This supports divi-child gallery Maps/Wikipedia features and Wayfarer map and
trip/timeline features after downstream privacy decisions. Raw EXIF remains
excluded, public preview bytes remain metadata-minimal, and no public metadata
route is added. #26 is independent from #5 deployment acceptance.

Actual WordPress and Wayfarer integration remains in their own repositories.
No geocoding, per-asset overrides, publication database or cache is introduced.

## V1 candidates — only with evidence

### Video delivery

Add configured video-policy support only after specifying and testing:

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
