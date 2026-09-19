# Changelog

## Unreleased

## 1.1.0 - 2026-09-19

- Breaking: replace the former loopback catalog routes with public GET-only
  `/catalog/` discovery and credential-free CORS. Policy, lifecycle and metadata
  privacy checks remain authoritative; nginx retains shared admission protection.
- Remove unused `server.public_base_url`; clients use one Gateway origin and
  relative media paths. Update configuration, clients and nginx together.
- Standardize catalog terminology and update bundled guidance/smoke tooling.
- Release #66 as a documented pre-adoption v1 contract correction: no accepted
  downstream consumer depends on the former contract; the first WordPress
  consumer is blocked pending this correction. The Go module path is unchanged.

## 1.0.2 - 2026-09-19

- Clarify operator and application-integrator documentation paths and deployment navigation.
- Show how discovered preview and privacy-enabled original paths become public URLs.
- Make release-download examples version-neutral and preserve the offline documentation journey.
- Add a concise repository description and factual discovery topics.

## 1.0.1 - 2026-09-19

- Add optional `privacy.expose_source_metadata`, default off, guarding source metadata.
- Keep sensitive trusted timestamps, coordinates and original capability keys present
  as null while off; exact image/video originals are unavailable without provider fetches.
- Preserve qualified previews/posters; explicit opt-in enables validated sensitive
  fields and exact originals, whose bytes may contain further embedded metadata.
- Make smoke checks verify the intended source-metadata mode, defaulting to off.
- Simplify public documentation to current product and configuration terminology.

## 1.0.0 - 2026-09-18

First stable release for Linux amd64.

- Named provider roots with global and root-scoped exact directory publication
  rules, fail-closed configuration and fresh lifecycle/policy checks.
- Paginated trusted image/video collections and catalog with safe metadata
  and stable gateway URLs for same-host consumers.
- Image previews and video posters plus exact authorized image/video originals;
  originals preserve embedded source metadata, including EXIF/GPS.
- Validated video byte ranges and inactivity-bounded original streaming.
- Hardened unprivileged systemd and loopback/nginx deployment templates that
  keep the provider and trusted catalog private, with an aggregate nginx
  public-origin abuse envelope for request rate and concurrency.
- Deterministic bundle packaging, checksums, portable smoke checks and documented
  install, upgrade and rollback procedures.
- Operator and consumer GitHub Pages documentation, also available offline in
  the release bundle.
