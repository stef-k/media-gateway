# AGENTS.md

This file defines Media Gateway-specific rules for coding agents. General Git safety, review discipline and globally supplied agent capabilities are intentionally not duplicated here.

## Repository authority

Before consequential implementation or issue hardening, read:

1. `README.md`
2. `docs/product-completion.md` for #37 work
3. `docs/architecture.md`
4. `docs/security.md`
5. `docs/toolchain.md`
6. `docs/logging.md`
7. `docs/deployment.md` when work touches host integration
8. `docs/roadmap.md`
9. this file
10. the owning epic and implementation issue

The accepted V0 implementation remains authoritative for **current behavior**. `docs/product-completion.md` and #37/#38–#42 are authoritative for the **target product contract**. Do not silently treat planned routes/schema as already implemented.

If implementation and documentation disagree on a security boundary, public/consumer contract, policy semantics, selected toolchain, logging/privacy rule or dependency policy, resolve the inconsistency explicitly and update the relevant authority document in the same change.

## Product boundary

Media Gateway is a deliberately small convention-driven publication boundary between a private media provider and public HTTP delivery.

Immich is the initial provider. Operators identify publishable media through configured provider-root and exact directory-component conventions. Trusted same-host consumers browse/select only currently eligible media. Consumer selection never grants publication authority.

Do not turn the project into:

- a media manager/gallery/editor;
- a replacement for Immich;
- a generic reverse proxy or URL fetcher;
- a NAS browser/direct filesystem server;
- a publication/selection database;
- a web UI;
- a WordPress/Wayfarer plugin;
- an arbitrary image/video transformation service.

Keep the provider seam small and concrete; do not build a plugin framework before another provider establishes real requirements.

## Technical baseline

Follow `docs/toolchain.md`.

The settled foundation is:

- Go 1.27 family, initial reviewed toolchain Go 1.27.1;
- one binary;
- one strict TOML configuration file;
- a separately protected provider credential;
- standard library first;
- `github.com/pelletier/go-toml/v2` as the expected TOML runtime dependency;
- `net/http`, `log/slog`, standard `testing`/fuzz support;
- no database;
- no container requirement;
- systemd + nginx reference deployment;
- loopback-only application listener.

Do not add a web framework, DI container, logging framework, ORM, job system, configuration framework or other runtime dependency unless the owning issue establishes a concrete need the standard library cannot reasonably satisfy.

Toolchain/dependency upgrades are deliberate reviewed changes, not opportunistic agent updates.

## Security invariants

These are not ordinary configuration choices:

- deny by default;
- Immich/provider remains private;
- never accept an arbitrary upstream URL from a public or consumer request;
- never accept/open a filesystem path supplied by a caller;
- never mount/read the NAS directly as part of normal product operation;
- publication requires a configured provider root plus an exact eligible directory convention and media type;
- directory conventions remain configurable literals and must never degrade into prefix/glob/regex semantics such as `public*`;
- media type comes from provider metadata, not filename alone;
- lifecycle and publication authorization are reevaluated from current provider metadata for every public delivery request;
- consumer collection/root/asset selectors and stored references never grant publication permission;
- private/missing/invalid assets remain non-enumerating denials;
- public HTTP supports only explicitly designed routes/methods/headers;
- provider credentials, provider absolute paths, raw provider errors and private metadata never enter public responses/routine logs;
- no endpoint may turn an arbitrary provider asset ID into bytes without current policy evaluation.

Tests must include known-private assets, outside-root/near-match paths, lifecycle revocation and malformed/crafted route/path/header attempts.

## Publication policy

### Current Policy v2 (#38)

Use named roots and global/root-scoped rules. Obsolete `policy.allowed_roots` is
rejected with a sanitized migration error; no compatibility alias exists:

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
segment = "post"
media = ["image"]
roots = ["images"]
```

Root paths are canonical provider-reported POSIX metadata paths, never local mount paths. Root names are stable logical identifiers. Omitted rule roots mean global; explicit root lists cage a convention. Configured roots must not overlap.

A matching segment may occur at any descendant depth but must be exact component equality (`post` never matches `post process`).

The canonical `publication.Evaluate` returns eligibility plus logical root name and
root-relative parent collection path; denial returns zero context. The trusted catalogue projects this context only after eligibility succeeds. Root names are unique
1..64 lowercase ASCII letters/digits/hyphens with alphanumeric ends. `/` and
overlapping root paths are invalid. Explicit empty scopes and duplicate
segment/unordered-scope pairs fail startup; distinct scopes combine with OR semantics.

## Public and private surfaces

The same process may host public delivery and localhost-only consumer routes, but nginx publishes only the explicit public media surface.

Never expose `/internal/`, provider search/API/UI, configuration, diagnostics containing provider data or control operations through the public vhost.

No public health/readiness surface is required merely for convention.

### Product-complete public routes

#37 requires fixed gateway representations:

```text
GET/HEAD /media/<id>/preview
GET/HEAD /media/<id>/original
```

`preview` is a provider-generated browse/picker/poster representation.

`original` means provider original bytes after current authorization. It must never silently mean preview, transcoded playback or another derivative. Original delivery does not imply metadata stripping; source EXIF/GPS/etc. are part of authorized original bytes.

#30 may later add additional explicit browser-safe derivatives, but it is not a substitute for `/original`.

## Trusted consumer catalogue

#39 implements the generic loopback catalogue:

```text
GET /internal/collections?limit=<n>&cursor=<opaque>
GET /internal/assets?root=<logical-root>&collection=<relative-path>&limit=<n>&cursor=<opaque>
GET /internal/assets/<id>
```

Large collections must be paginated. Keep a small default page and hard maximum; internal provider scanning/filling must itself be bounded.

Safe projection may include logical root name, root-relative collection path, filename, image/video type, dimensions/duration, times, validated nullable coordinates and stable preview/original gateway paths.

Never expose provider absolute paths, provider URLs, credentials, raw EXIF or provider JSON.

Coordinates are target normal trusted metadata, not publication authority. Preserve strict pair/range validation and never log them.

Provider search filters are optimization only. Every returned asset must pass current policy/lifecycle checks.

Current pages default to 25 (maximum 100), with at most eight provider calls and 30 seconds of work. Gateway HMAC cursors bind selectors/policy and are invalidated by restart. Collections are deduplicated within a page only; consumers merge at-least-once results by `(root, collection_path)`. Asset `duration_ms` is nullable integer milliseconds. Coordinates are always present as a validated nullable pair; no `[consumer]` config remains. Image/video preview and original capabilities are non-null. Projection performs no representation probes and never grants delivery authorization. Use structured metadata search, never Immich folder view.

## Provider integration

Immich access belongs behind a small provider boundary. **Verify the currently supported/deployed Immich API immediately before implementing or changing endpoints/search/representation/range behavior.** Do not encode assumptions from old examples or previous chats.

Use fixed configured provider authority and dedicated least-privilege credential. Do not use administrator credentials when narrower permissions suffice.

Use bounded connect/header/metadata operations and explicit response validation. Provider failures become sanitized gateway failures, never arbitrary provider responses.

## Original and video delivery

#40 implements image GET/HEAD originals using provider GET `/api/assets/<UUIDv4>/original`, no query, and `asset.download`. The dedicated key union is `asset.read` + `asset.view` + `asset.download`; no administrator/write permission. Accept only direct 200, one parameter-free `image/*` content type and one explicit positive int64 length, no encoding/range headers, no size ceiling or fallback. HEAD closes the provider GET body after header validation. #41 extends this same seam to video, accepting parameter-free `video/*` or `application/mxf`. Fixed video posters use only thumbnail `size=preview`. Never substitute playback/transcoded bytes. M6 #41 qualification and cleanup remain mandatory before merge.

Do not smuggle either into unrelated policy/catalogue changes.

Only after video-original authorization, parse at most one Range value of at most 128 bytes: `bytes=first-last`, `bytes=first-`, or `bytes=-positive-suffix`, unsigned signed-int64 decimals, no whitespace/signs/commas. Malformed input is fixed 400 with no original open; denied assets remain 404 first. Canonical Range is the only caller-derived provider header. Validate exact 206 interval mathematics and positive length/total; valid unsatisfiable 416 has `bytes */total` and a public zero body. Immich 3.2.0 range 404 triggers exactly one fixed no-Range original GET, without query/caller headers. Validate normal video 200 framing and resolve the range against its positive length. Unsatisfiable math closes the body and yields public 416. A suffix length greater than or equal to the total reuses that validated original body as full-representation 206 with `Content-Range: bytes 0-(total-1)/total`; HEAD closes it without reading. Other satisfiable ranges close the body and yield 502. No third request is made. Probe 404 remains public 404; all other probe failures yield sanitized 502. Normal 206 does not probe; no loops or playback. Provider 200 for a range is 502, never fallback. HEAD performs the same provider GET and closes it. Image originals ignore Range and retain full 200 without Accept-Ranges. Previews ignore Range; all conditionals remain unsupported and stripped.

If provider original download cannot satisfy browser video range needs, do not redefine `/original`; design any separate playback representation explicitly.

### Streaming lifetime

#41 establishes fixed 60-second upstream read and downstream write inactivity bounds for image/video originals, refreshed per I/O. Original connection/header work remains bounded; metadata/search/preview retain their normal timeouts. Ordinary responses keep the finite 65-second server write default. Shutdown retains its 10-second drain, then forces remaining connections closed. nginx forwards Range only for original and retains finite 70-second read/65-second send inactivity bounds. No range/conditional values enter logs.

Authorization, provider connect/header acquisition and metadata work remain hard-bounded. Once an authorized media stream is established, longer transfer lifetime should be bounded by I/O inactivity, client disconnect, provider transport safety and bounded shutdown rather than a short absolute wall-clock deadline. Stalled connections must still be finite.

nginx must mirror only the accepted public route/header semantics and retain fixed gateway upstream/no storage fallback.

## Logging

Follow `docs/logging.md`.

- nginx owns public request/access and proxy-transport logging;
- Go uses `log/slog` for lifecycle, sanitized startup/provider/runtime/security diagnostics;
- journald owns reference application log persistence/rotation;
- do not add an application-managed logfile or duplicate access log;
- denied/malformed Internet traffic must not become an unbounded warning/error flood;
- never log credentials, auth headers, full provider/NAS paths, GPS/EXIF or provider error bodies.

## Configuration and secrets

Configuration remains human-readable TOML with strict unknown-field rejection. The obsolete `[delivery]` table fails with sanitized guidance to remove it: image preview/original are fixed representations, with no runtime feature gate. Invalid/ambiguous authorization config fails startup.

Committed operator examples must comment deployment-specific values, trust boundaries and non-obvious hardening choices.

Never commit credentials. Secrets live outside the repository with restrictive host permissions.

Configuration is startup state. Do not add hot reload unless a later issue establishes a real need.

## Operations

Run as a dedicated unprivileged account bound only to numeric loopback. nginx is the Internet-facing boundary.

Deployment examples are templates; preserve unrelated host services and validate systemd/nginx before restart/reload.

The accepted V0 bundle/smoke/upgrade/rollback discipline remains the baseline for #42 final product qualification.

## Scope and issue execution

Historical V0 tracker #1 is closed and should remain closed as the accepted security/deployment milestone.

Current product-completion authority is #37 with bounded children:

```text
#38 -> #39 -> #40 -> #41 -> #42 -> close #37
```

- #38 named roots + global/root-scoped policy
- #39 paginated image/video collections/catalogue
- #40 original image delivery
- #41 video preview/original, ranges and long streaming
- #42 final docs/config/bundle/smoke/real-host qualification

Do not hand #37 wholesale to an implementation agent while these children exist. Consult live issue state before choosing the next unit.

#30 remains optional later derivative-profile work and must not replace or delay required `/original` semantics without concrete evidence.

The real `media.stefk.me`/Cloudflare cutover in `stef-k/server-migration#10` should wait for #37 completion.

Documentation is part of completion whenever policy/configuration, public/consumer contracts, provider endpoints, delivery/streaming, deployment, logging, toolchain/dependencies or security behavior changes.
