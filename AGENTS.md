# AGENTS.md

This file defines Media Gateway-specific rules for coding agents. General Git safety, review discipline, and globally supplied agent capabilities are intentionally not duplicated here.

## Repository authority

Before consequential implementation or issue hardening, read:

1. `README.md`
2. `docs/architecture.md`
3. `docs/security.md`
4. `docs/toolchain.md`
5. `docs/deployment.md` when work touches host integration
6. `docs/roadmap.md`
7. this file
8. the owning GitHub issue

If implementation and documentation disagree on a security boundary, public contract, selected toolchain or dependency policy, resolve the inconsistency explicitly and update the relevant authority document in the same change.

## Product boundary

Media Gateway is a deliberately small publication boundary between a private media provider and public HTTP delivery.

V0 uses Immich as the provider and targets image delivery first. Keep the design provider-aware without building a generic plugin system before a second provider exists.

Do not turn the project into:

- a media manager or gallery application;
- a replacement for Immich;
- a generic reverse proxy;
- a generic URL fetcher;
- a NAS browser;
- a publication database;
- a web UI;
- a WordPress plugin.

## V0 implementation direction

Follow `docs/toolchain.md`.

The settled initial baseline is:

- Go 1.27 family, initial reviewed toolchain Go 1.27.1;
- one binary;
- one TOML configuration file;
- a separately protected provider credential;
- standard library first;
- `github.com/pelletier/go-toml/v2` v2.4.3 as the expected V0 TOML dependency;
- `net/http` rather than a web/router framework;
- `log/slog` rather than a third-party logging framework;
- standard `testing`/fuzz support;
- no database;
- no container requirement;
- systemd + nginx deployment examples;
- loopback-only application listener.

Do not add a web framework, DI container, logging framework, ORM, background-job system, configuration framework or other runtime dependency unless the owning issue establishes a concrete requirement that the standard library cannot reasonably satisfy.

Go has no separate LTS channel. Use supported stable releases only. Toolchain/dependency upgrades are deliberate changes with tests and documentation, not opportunistic agent updates.

## Security invariants

These are not ordinary configuration choices:

- deny by default;
- the provider itself remains private;
- the gateway never accepts an arbitrary upstream URL;
- the gateway never accepts a filesystem path from a public request;
- the gateway never reads the NAS directly in V0;
- public delivery requires both an allowed provider root and an exact eligible directory segment;
- publication segment names are configurable literals, but matching must remain exact path-component equality and must never degrade into prefix/glob/regex behavior such as `public*`;
- media type must be validated from provider metadata, not trusted from a filename alone;
- authorization is reevaluated from current provider metadata for delivery rather than inferred from a consumer reference;
- consumer state, including WordPress attachment/reference state, never grants publication permission;
- private/missing/invalid assets return a non-enumerating denial such as `404`;
- public HTTP supports only the explicitly designed read methods and routes;
- provider credentials are never emitted to callers, logs, URLs, or public error responses;
- no endpoint may turn an arbitrary provider asset identifier into bytes without policy evaluation.

Tests must include attempts to request known private assets and malformed/crafted paths and must prove fail-closed behavior.

## Publication policy

The motivating policy uses provider-indexed path metadata beneath explicitly configured allowed roots.

Conceptually:

```text
allowed root: /media/archive       # deployment-specific example

.../public/...          image/video eligible
.../public-images/...   image eligible
.../public-videos/...   video eligible
anything else           private
```

Neither `/media/archive` nor the example segment names are hard-coded product requirements. Operators may configure different provider-visible roots and different literal publication segment names. Security semantics remain fixed: roots are normalized/path-aware, segments are exact components, and invalid/ambiguous policy fails startup.

V0 production delivery may intentionally support only a subset of the media types declared by policy, such as images first. Unsupported media must fail closed even if its directory would otherwise be eligible.

Treat the path as metadata for authorization. Do not expose provider paths in public URLs and do not depend on host NAS mount paths.

## Public and private surfaces

The service may expose public delivery routes and localhost-only consumer/control routes from the same process, but nginx must publish only the public delivery surface.

Do not expose search, diagnostics containing provider data, configuration, provider metadata, or control operations through the public vhost.

Keep health endpoints minimal and free of secrets/provider topology.

## Provider integration

Immich access belongs behind a small provider boundary. Verify the current supported Immich API immediately before implementing or changing endpoints; do not encode assumptions from old API examples.

Use bounded HTTP clients with explicit connect/request timeouts and body limits where applicable. Provider failures must become bounded gateway failures, not hanging public requests.

Do not use an administrator credential when a narrower provider credential can satisfy the required read operations.

## Delivery

Prefer existing safe provider-generated previews for the first image-delivery slice if they meet quality and metadata/privacy requirements. Prove this with representative files before relying on it.

Do not expose original files by default. If originals or additional transcoding/resizing are introduced later, require an explicit issue with privacy, cache, size, and failure semantics.

Video/range delivery is separate work and must not be smuggled into the image slice.

## Configuration and secrets

Configuration must be human-readable and reviewable. TOML is the V0 format.

Committed operator-facing configuration examples (`*.toml`, nginx, systemd and similar deployment assets) must contain useful inline comments explaining deployment-specific values, trust boundaries and non-obvious hardening choices. Do not leave security-sensitive sample directives unexplained merely because equivalent prose exists elsewhere.

Do not commit credentials. Example configuration uses placeholder paths/values only. Secrets must live outside the repository with restrictive host permissions.

Use strict TOML decoding and reject unknown fields. Invalid or ambiguous policy configuration must prevent startup rather than silently broaden access.

## Operations

The service should run as an unprivileged dedicated account and bind only to loopback. nginx is the Internet-facing reverse-proxy boundary.

Deployment examples under `deploy/` are templates, not a license to weaken host-specific controls. Preserve existing host services and validate nginx/systemd configuration before reload/restart.

## Scope and issue execution

Use tracker #1 and explicit issue dependencies as execution authority. Prefer a few coarse epics and small implementation issues beneath them only when needed; this project does not need the engineering hierarchy of larger applications.

Documentation is part of completion when public routes, policy semantics, configuration, deployment, toolchain/dependencies, or security behavior changes.
