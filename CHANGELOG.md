# Changelog

## Unreleased

- Add optional `privacy.expose_source_metadata`, default off, guarding source metadata.
- Keep sensitive trusted timestamps, coordinates and original capability keys present
  as null while off; exact image/video originals are unavailable without provider fetches.
- Preserve qualified previews/posters; explicit opt-in enables validated sensitive
  fields and exact originals, whose bytes may contain further embedded metadata.
- Simplify public documentation to current product and configuration terminology.

## 1.0.0 - 2026-09-18

First stable release for Linux amd64.

- Named provider roots with global and root-scoped exact directory publication
  rules, fail-closed configuration and fresh lifecycle/policy checks.
- Paginated trusted image/video collections and catalogue with safe metadata
  and stable gateway URLs for same-host consumers.
- Image previews and video posters plus exact authorized image/video originals;
  originals preserve embedded source metadata, including EXIF/GPS.
- Validated video byte ranges and inactivity-bounded original streaming.
- Hardened unprivileged systemd and loopback/nginx deployment templates that
  keep the provider and trusted catalogue private, with an aggregate nginx
  public-origin abuse envelope for request rate and concurrency.
- Deterministic bundle packaging, checksums, portable smoke checks and documented
  install, upgrade and rollback procedures.
- Operator and consumer GitHub Pages documentation, also available offline in
  the release bundle.
