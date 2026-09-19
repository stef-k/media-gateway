# AGENTS.md

This file defines Media Gateway-specific rules for coding agents. General Git safety, review discipline and globally supplied agent capabilities are intentionally not duplicated here.

## Repository authority

Before consequential implementation or issue hardening, read:

1. `README.md`
2. `docs/product-completion.md` for settled product scope
3. `docs/architecture.md`
4. `docs/security.md`
5. `docs/toolchain.md`
6. `docs/logging.md`
7. `docs/deployment.md` when work touches host integration
8. `docs/roadmap.md`
9. this file
10. the owning epic and implementation issue

Product completion is finished: #37 and #38–#42 are accepted historical context.
The implementation and product/security documents define settled behavior.
#48 release engineering is complete; immutable v1.0.0 is accepted. #60 owns the
bounded source-metadata privacy correction. Release changes must not
alter settled media/security semantics without a separate bounded issue.

If implementation and documentation disagree on a security boundary, public/consumer contract, policy semantics, selected toolchain, logging/privacy rule or dependency policy, resolve the inconsistency explicitly and update the relevant authority document in the same change.

## Product boundary

Media Gateway is a deliberately small convention-driven publication boundary between a private media provider and public HTTP delivery.

Immich is the initial provider. Operators identify publishable media through configured provider-root and exact directory-component conventions. Public consumers browse/select only currently eligible media. Consumer selection never grants publication authority.

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

All external GitHub Actions must be pinned to reviewed full-length commit SHAs;
mutable action branches/tags are not acceptable. Action-pin updates must verify
the commit belongs to the intended upstream repository and be deliberately reviewed.

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

### Publication policy (#38)

Use named roots and global/root-scoped rules. Unknown/unsupported fields are
strictly rejected with sanitized errors; no compatibility aliases exist:

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
root-relative parent collection path; denial returns zero context. The public catalog projects this context only after eligibility succeeds. Root names are unique
1..64 lowercase ASCII letters/digits/hyphens with alphanumeric ends. `/` and
overlapping root paths are invalid. Explicit empty scopes and duplicate
segment/unordered-scope pairs fail startup; distinct scopes combine with OR semantics.

## Public and private surfaces

The process hosts public catalog and media delivery routes on numeric loopback; nginx publishes only those explicit routes.

Never expose provider search/API/UI, configuration, diagnostics containing provider data or control operations through the public vhost.

No public health/readiness surface is required merely for convention.

### Product-complete public routes

The accepted product provides fixed gateway representations:

```text
GET/HEAD /media/<id>/preview
GET/HEAD /media/<id>/original
```

`preview` is a provider-generated browse/picker/poster representation.

`original` means provider original bytes after current authorization. It must never silently mean preview, transcoded playback or another derivative. Original delivery does not imply metadata stripping; source EXIF/GPS/etc. are part of authorized original bytes.

The settled media surface is catalog + preview/poster + privacy-permitted exact
original. Consumers own browsing, selection, presentation and consumer-specific
derivative/caching strategy.

## Public catalog

#66 exposes the policy-filtered public catalog, replacing the historical #39 loopback contract:

```text
GET /catalog/collections?limit=<n>&cursor=<opaque>
GET /catalog/assets?root=<logical-root>&collection=<relative-path>&limit=<n>&cursor=<opaque>
GET /catalog/assets/<id>
```

Catalog responses grant credential-free `Access-Control-Allow-Origin: *`; GET is the only method.
Large collections must be paginated. Keep a small default page and hard maximum; internal provider scanning/filling must itself be bounded.

Safe projection may include logical root name, root-relative collection path, filename, image/video type, dimensions/duration, times, validated nullable coordinates and stable preview/original gateway paths.

Never expose provider absolute paths, provider URLs, credentials, raw EXIF or provider JSON.

Coordinates are source-sensitive metadata, null by default and validated/projected
only when `privacy.expose_source_metadata=true`. They never grant publication
authority and must never enter logs.

Provider search filters are optimization only. Every returned asset must pass current policy/lifecycle checks.

Current pages default to 25 (maximum 100), with at most eight provider calls and 30 seconds of work. Gateway HMAC cursors bind selectors/policy and are invalidated by restart. Collections are deduplicated within a page only; consumers merge at-least-once results by `(root, collection_path)`. Asset `duration_ms` is nullable integer milliseconds. Sensitive timestamps, coordinates and original paths are present but null by default. Preview paths remain non-null. Projection performs no representation probes and never grants delivery authorization. Use structured metadata search, never Immich folder view.

## Provider integration

Immich access belongs behind a small provider boundary. **Verify the currently supported/deployed Immich API immediately before implementing or changing endpoints/search/representation/range behavior.** Do not encode assumptions from old examples or previous chats.

Use fixed configured provider authority and dedicated least-privilege credential. Do not use administrator credentials when narrower permissions suffice.

Use bounded connect/header/metadata operations and explicit response validation. Provider failures become sanitized gateway failures, never arbitrary provider responses.

## Source-metadata privacy

The optional `[privacy] expose_source_metadata = false` setting defaults to false.
It controls capture/local timestamps, coordinates, public original capability and
exact image/video originals, not publication eligibility. Discovery always uses
`withExif=false`; list/detail follows the immutable startup setting. While off,
hidden sensitive values are ignored without validation and project as present/null;
dimensions/duration still validate. Originals return fixed 404 before provider I/O
or Range parsing. Qualified previews/posters and filename/collection identity remain.

True deliberately publishes validated timestamps/nullable coordinate pairs and enables the existing
exact-original contract, including arbitrary embedded source metadata. Never infer
original safety from absent coordinates or add transformations. No caller-controlled
metadata selectors, globals or privacy state in cursors; restart invalidates signing keys.

## Original and video delivery

#40 implements image GET/HEAD originals using provider GET `/api/assets/<UUIDv4>/original`, no query, and `asset.download`. The dedicated key union is `asset.read` + `asset.view` + `asset.download`; no administrator/write permission. Accept only direct 200, one parameter-free `image/*` content type and one explicit positive int64 length, no encoding/range headers, no size ceiling or fallback. HEAD closes the provider GET body after header validation. #41 extends this same seam to video, accepting parameter-free `video/*` or `application/mxf`. Fixed video posters use only thumbnail `size=preview`. Never substitute playback/transcoded bytes. M6 #41 qualification and cleanup passed before PR #47 merged; #42 final bundle qualification is complete.

Do not smuggle either into unrelated policy/catalog changes.

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

Configuration remains human-readable TOML with strict unknown-field rejection.
Invalid/ambiguous authorization config fails startup.

Committed operator examples must comment deployment-specific values, trust boundaries and non-obvious hardening choices.

Never commit credentials. Secrets live outside the repository with restrictive host permissions.

Configuration is startup state. Do not add hot reload unless a later issue establishes a real need.

## Operations

Run as a dedicated unprivileged account bound only to numeric loopback. nginx is the Internet-facing boundary.

Deployment examples are templates; preserve unrelated host services and validate systemd/nginx before restart/reload.

The accepted bundle/smoke/install/upgrade/rollback discipline remains the baseline
for versioned distribution. Release validation does not constitute deployment.

## Scope and issue execution

Historical milestone tracker #1 is closed and should remain closed as the accepted security/deployment milestone.

Product completion (#37, including #38–#42) is finished. #48 owns formal
immutable tag versions, CHANGELOG, GitHub Releases and operator distribution.
Keep normal CI exact-revision qualification separate from tag publication.
Tags are the sole version authority; never add a duplicate version constant/file.
The accepted `v1.0.0` tag/Release is immutable. #60 must not tag, publish or deploy;
a patch release requires a separate accepted preparation/review step.

#30 is closed as not planned. A measured new representation need requires a fresh
bounded issue; additional gateway derivatives are not standing backlog.
Release changes must not alter settled media/provider/policy/HTTP/security or smoke
semantics without a separate bounded issue. Production deployment is separate.
Consult live issue state before choosing the next unit.

Documentation is part of completion whenever policy/configuration, public/consumer contracts, provider endpoints, delivery/streaming, deployment, logging, toolchain/dependencies or security behavior changes.
