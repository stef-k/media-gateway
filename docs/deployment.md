# Deployment

This document describes the reference deployment shape. The files under `deploy/` are templates, not unattended installers.

## Filesystem layout

A simple Linux installation can use:

```text
/usr/local/bin/media-gateway
/etc/media-gateway/config.toml
/etc/media-gateway/immich.key
/etc/systemd/system/media-gateway.service
/etc/nginx/sites-available/media-gateway
```

Runtime/cache directories should be added only when a feature actually needs them.

## Service account

Run the gateway as a dedicated unprivileged account, for example:

```text
media-gateway
```

The service requires:

- execute access to the binary;
- read access to the non-secret TOML configuration;
- read access to the provider credential;
- network access to the configured private provider and loopback listener.

It does **not** require access to the NAS/archive filesystem in V0.

A typical credential file should be owned/readable only by root and the service identity as appropriate for the host. Never place the credential in Git or in an nginx configuration.

## Configuration

Start from the repository's [`deploy/config.toml.example`](https://github.com/stef-k/media-gateway/blob/main/deploy/config.toml.example) and follow the [validated configuration contract](configuration.md).

The reference shape keeps secrets separate:

```toml
[provider]
api_key_file = "/etc/media-gateway/immich.key"
```

Changing the publication policy is security-sensitive configuration work. Validate configuration before/restart and test at least one known-private and one known-public asset afterward.

## systemd

The reference [`deploy/media-gateway.service`](https://github.com/stef-k/media-gateway/blob/main/deploy/media-gateway.service) is intentionally conservative:

- dedicated user/group;
- loopback service;
- no privileged capabilities;
- `NoNewPrivileges` and standard filesystem/kernel hardening;
- explicit configuration path;
- restart on unexpected failure.

The executable requires `-config <file>`, matching the service template. `-version`
prints build provenance without loading configuration; `-h` prints usage. Positional
arguments and invalid flags are rejected without echoing their values. Exit codes
are 0 for help/version or clean signal shutdown, 2 for CLI misuse, and 1 for
configuration, bind, serve or shutdown failure.

SIGINT/SIGTERM close the listener and drain active requests for up to 10 seconds;
on timeout the process closes connections and exits with failure. This fits inside
`TimeoutStopSec=30s`. Configuration changes require a process restart.

Only `GET`/`HEAD /media/<asset-id>/preview` can deliver media. `/health`, search,
public metadata/control routes and unsupported methods remain fixed denials.
The [consumer API](consumer-api.md) adds only loopback `/internal/assets` browse
and detail routes; nginx must never publish `/internal/`. There is
no startup provider probe: `listening` means the loopback listener was acquired,
not that previews are qualified or available. HTTP limits are 5 seconds for
headers, 10 seconds for request reads, 65 seconds for response writes, 30 seconds
for idle connections, and 16 KiB for headers (plus Go's parsing allowance). No
handler reads request bodies. A 60-second handler context bounds combined
metadata/preview work; each provider call also retains its configured timeout
through body reading. The finite write deadline bounds slow public readers.
Client disconnects cancel upstream work. Shutdown retains the 10-second drain.

Preview bodies must have a known positive length of at most 16 MiB and an exact
JPEG/WebP content type. Headers are validated before streaming; a short body or
mid-stream failure aborts the response. HEAD checks current metadata/policy and
preview headers, then closes the body. All responses use `no-store`; configure
nginx/edge to honor it and never force-cache these routes. Private/missing/invalid
assets get `404`; provider/auth/validation failures before headers get `502`.
Do not enable production use until #18's real-Immich privacy/quality gate passes.

Before installation/reload:

```bash
sudo systemd-analyze verify /etc/systemd/system/media-gateway.service
sudo systemctl daemon-reload
```

Start the service only after the provider credential and validated configuration exist.

## nginx

nginx is the public HTTP boundary; Media Gateway itself remains loopback-only.

The reference [`deploy/nginx.conf`](https://github.com/stef-k/media-gateway/blob/main/deploy/nginx.conf) demonstrates a dedicated origin vhost that:

- proxies only `/media/` to the gateway;
- accepts only `GET`/`HEAD` for the public media path;
- returns `404` everywhere else;
- does not expose internal search/control routes;
- uses bounded proxy timeouts.

The example listens on loopback for compatibility with a tunnel/reverse-proxy edge. Direct Internet deployments may instead terminate TLS in nginx, but they must preserve the same route boundary.

Validate before reload:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

## Logging

Logging is intentionally split between nginx and the Go service. See [`docs/logging.md`](logging.md) for the authoritative privacy/severity rules.

### nginx

nginx owns the public HTTP access/error record for the published media route. It is the right place to observe client/method/status/bytes/timing and reverse-proxy transport failures.

Do not put provider credentials or authorization headers into access logs. If a future feature introduces signed/tokenized query parameters, review the nginx log format before enabling it so signatures/tokens are not persisted accidentally.

### Media Gateway

The Go process uses the standard library `log/slog` for lifecycle, sanitized startup/configuration state, provider/runtime failures and security-relevant diagnostics. It writes to stderr/stdout; under systemd, journald owns persistence and rotation.

There is no V0 application-managed logfile or rotation subsystem and no second full access log duplicating nginx.

Useful operator commands include:

```bash
journalctl -u media-gateway
journalctl -u media-gateway -f
```

Routine denied/malformed Internet traffic must not become an unbounded high-severity application-log flood. Application logs must not contain provider API keys, authorization headers, private provider/NAS paths, GPS/EXIF metadata or provider error bodies.

## HTTPS / edge

The motivating deployment uses a dedicated public hostname such as:

```text
https://media.example.com
```

Public HTTPS can terminate at Cloudflare, nginx, or another trusted edge. The edge must route only to the nginx Media Gateway vhost; it must not route directly to Immich.

For Cloudflare Tunnel, point the hostname at the dedicated nginx origin listener. Do not create a tunnel route to the Immich application port.

## Provider connectivity

A local Immich deployment may be configured as:

```text
http://127.0.0.1:2283
```

Other private-network layouts are valid, but the provider URL is operator configuration and cannot be supplied by public requests.

### Reviewed metadata contract

Verified on 2026-09-12 against official Immich OpenAPI version **3.2.0** at
[upstream commit `0f901eea5ec2d3ebf85188b8c1dd193ae3619966`](https://github.com/immich-app/immich/blob/0f901eea5ec2d3ebf85188b8c1dd193ae3619966/open-api/immich-openapi-specs.json).
The [Retrieve an asset operation](https://api.immich.app/endpoints/assets/getAssetInfo)
(`getAssetInfo`) uses `GET /api/assets/{id}` and returns HTTP `200` with
`application/json`. The API server prefix is `/api`; configured `base_url`
remains an origin, not an API path.

Use a non-administrator account with access to the intended assets and an API key
restricted to **`asset.read` + `asset.view`**, sent only in the **`x-api-key`** header. This is the
union required by the implemented metadata (`asset.read`) and preview
(`asset.view`) operations; it does not override Immich's asset-access checks.
No administrator, original-download or write permission is required. Candidate
search also uses `asset.read`; it introduces no additional permission.

The OpenAPI and [controller](https://github.com/immich-app/immich/blob/0f901eea5ec2d3ebf85188b8c1dd193ae3619966/server/src/controllers/asset.controller.ts)
require UUIDv4 identifiers: hyphenated hexadecimal `8-4-4-4-12`, version nibble
`4`, variant nibble `8`, `9`, `a` or `b` (either letter case). The adapter checks
this before sending requests. Returned IDs must denote the same UUID; letter
case alone is insignificant.

Only `AssetResponseDto.id`, `originalPath` and `type` are retained. All are required
and non-empty. `IMAGE` maps to `image`, `VIDEO` to `video`; documented `AUDIO` and
`OTHER`, and any unknown type, fail closed. Paths pass unchanged to
`publication.Eligible`; neither ID knowledge nor successful metadata retrieval
grants publication. Video mapping does not enable video delivery.

`internal/immich.New` consumes validated `config.Load` provider values and its
separately returned credential. `Client.Asset` performs a fresh lookup each time.
It uses no environment proxy, follows no redirects, and emits no logs. The
configured request timeout covers connection through response-body reading;
caller cancellation is honored. Connection and TLS handshakes additionally have
a five-second maximum (or the shorter configured timeout). Response headers are
limited to 16 KiB and the complete metadata body to 1 MiB before JSON decoding,
including unrelated fields. Oversized or malformed responses fail closed.

Errors expose only fixed outcome classes: invalid ID, missing (`400`/`404` in `Asset()`), auth
(`401`/`403`), unexpected provider status, transport failure, invalid metadata or
unsupported media. Cancellation and deadline errors are standard context
sentinels. No upstream URL, body, key or private path is embedded in errors.
The HTTP handler maps invalid/missing/unsupported outcomes to fixed 404 denials
and other failures to fixed 502 responses. No public metadata endpoint exists.

Immich v3.2.0 metadata lookup deliberately returns HTTP 400 for missing assets
or absent `asset.read` access: see [the access gate](https://github.com/immich-app/immich/blob/v3.2.0/server/src/utils/access.ts)
and [asset retrieval](https://github.com/immich-app/immich/blob/v3.2.0/server/src/services/asset.service.ts).
For this fixed UUIDv4-prevalidated endpoint only, `Asset()` maps 400 alongside 404
to `ErrMissing` without reading provider error bodies. Preview and candidate-search
status handling are unchanged. #18 still requires the post-merge real-provider
missing-UUID probe before qualification can complete.

This verification is an upstream API/source review plus local HTTP contract tests,
not qualification against a deployed Immich instance. Re-check API permissions
and run provider/policy smoke tests when upgrading Immich.

### Reviewed candidate search contract

Re-verified on **2026-09-13** against official Immich OpenAPI **3.2.0** at
[commit `0f901eea5ec2d3ebf85188b8c1dd193ae3619966`](https://github.com/immich-app/immich/blob/0f901eea5ec2d3ebf85188b8c1dd193ae3619966/open-api/immich-openapi-specs.json).
`searchAssets` uses only `POST /api/search/metadata`, `x-api-key`, permission
`asset.read`, and a direct HTTP 200 `application/json` response. The
[controller](https://github.com/immich-app/immich/blob/0f901eea5ec2d3ebf85188b8c1dd193ae3619966/server/src/controllers/search.controller.ts)
and [service](https://github.com/immich-app/immich/blob/0f901eea5ec2d3ebf85188b8c1dd193ae3619966/server/src/services/search.service.ts)
confirm the structured request and cursor response path.

`Client.SearchCandidates(ctx, CandidateQuery{Limit: 25})` constructs:

```json
{
  "filter": {"type": {"eq": "IMAGE"}},
  "orderBy": {"field": "fileCreatedAt", "direction": "desc"},
  "size": 25,
  "withExif": false,
  "withPeople": false,
  "withStacked": false
}
```

The gateway fixes all filter/operator/order fields; there is no caller-selected
path, URL, type, sort or arbitrary search tree. It uses no deprecated flat search
fields. An optional `ID` adds only `filter.id.eq` after UUIDv4 validation, requires
no cursor, and permits at most one matching result (case-insensitive UUID match).
An absent exact candidate is a successful empty page. `Limit` must be 1–100,
smaller than Immich's schema maximum of 1000. No retries or automatic page scans
occur. Ordering is newest `fileCreatedAt` first; the reviewed
[ordering implementation](https://github.com/immich-app/immich/blob/0f901eea5ec2d3ebf85188b8c1dd193ae3619966/server/src/utils/database.ts)
uses asset ID as its deterministic tie-break. Pagination is not a stable snapshot
of a changing provider library.

A nonempty `Cursor` is passed only in the JSON body's `cursor` field. Both input
and returned cursors must be valid UTF-8, at most 1024 bytes, and contain no Unicode
control characters. They remain opaque and must not be logged. The adapter
requires `assets.items`, matching `assets.count`, and `assets.nextCursor` (a
nonempty bounded string or explicit `null` for completion). It returns
`CandidatePage.Items` and `NextCursor`; terminal `null` maps to an empty string.

Every item requires a UUIDv4 ID, nonempty unchanged `originalPath`, exact `IMAGE`
type (mapped to `image`), `width`, `height`, `fileCreatedAt` and `localDateTime`.
Dimensions use the schema's nullable nonnegative integer range through
9007199254740991: `null` means unknown, and zero is preserved. Times must parse
as RFC3339 with optional fractional seconds and are preserved as strings.
`fileCreatedAt` represents capture time; `localDateTime` retains the provider's
local wall-clock semantics and is not converted into another timezone. No EXIF,
GPS, people or unrelated provider metadata enters the candidate result.

The existing fixed origin, credential-safe transport, redirect rejection,
timeouts and cancellation apply. The entire JSON body, including ignored fields,
is capped at 1 MiB before decoding; excess candidates, invalid shapes, unsupported
media and malformed fields reject the whole page. Search uses the existing fixed
error classes plus `ErrSearchQuery` for invalid pagination/query inputs. It emits
no logs and exposes no provider values through errors.

**Candidate discovery is not authorization.** Private, outside-root and crafted
paths can be returned unchanged for later `publication.Eligible` evaluation.
Every item must pass that evaluation before a consumer receives it; provider
search filters and consumer references never grant permission. #20 adds no
localhost or public consumer HTTP API. Source review and fake-provider tests do
not change #18/#4 qualification state or establish production preview safety.

### Reviewed preview contract

Re-verified on 2026-09-12 against the same current official upstream commit
`0f901eea5ec2d3ebf85188b8c1dd193ae3619966`. The OpenAPI `viewAsset` operation is
`GET /api/assets/{id}/thumbnail?size=preview`, with `asset.view` permission and
`x-api-key` authentication. UUIDv4 validation and the configured origin remain
identical to metadata lookup. The
[controller](https://github.com/immich-app/immich/blob/0f901eea5ec2d3ebf85188b8c1dd193ae3619966/server/src/controllers/asset-media.controller.ts)
and [service](https://github.com/immich-app/immich/blob/0f901eea5ec2d3ebf85188b8c1dd193ae3619966/server/src/services/asset-media.service.ts)
show thumbnail redirects and original/fullsize fallback behavior. Media Gateway
rejects every redirect and sends only the fixed `size=preview` query, never caller
headers, queries or authorization. It requests identity encoding.

OpenAPI describes the body generically as `application/octet-stream`; the service
sets its actual type from the derivative path. Official
[image settings](https://docs.immich.app/administration/system-settings/) describe
JPEG/WebP previews. V0 therefore accepts only exact `image/jpeg` and `image/webp`,
not the generic OpenAPI body type. The 16 MiB bound is a gateway V0 limit, not an
Immich guarantee. Missing previews deny; redirects and invalid representation
headers fail with 502. No retry, alternate representation or storage fallback exists.

Issue #18 must record the deployed Immich version/settings and representative
quality, metadata/privacy, direct-response and revocation evidence through this
exact gateway route. Fake-provider CI and upstream source review do not supply
that evidence. The service makes no claim that previews strip sensitive metadata.

## Host firewall

The gateway listener should not require a firewall opening when bound to `127.0.0.1`.

Only the chosen nginx/edge origin must be reachable by the component that fronts it. Avoid broad `0.0.0.0` listeners when the deployment does not require them.

## Validation checklist

Before declaring a deployment usable:

1. gateway binds only to the intended loopback address;
2. nginx publishes only the expected hostname/path;
3. provider remains unreachable through the public hostname;
4. known eligible test image returns successfully;
5. known private asset ID returns `404`;
6. asset outside the allowed provider root returns `404`;
7. configured publication-segment near misses return `404`;
8. public image response has the expected content type and no sensitive EXIF/GPS metadata;
9. provider outage produces bounded failure and no storage fallback;
10. secrets/private provider paths do not appear in nginx or journald logs or responses;
11. nginx access logging records the intended public request facts without secret query/header material;
12. gateway lifecycle/provider failures are visible through `journalctl -u media-gateway`;
13. repeated denied requests do not create an obvious unbounded high-severity application-log flood;
14. service survives/restarts cleanly under systemd;
15. unrelated host services remain healthy.

## Upgrades

Keep upgrades deliberate:

1. review release notes and security-impacting changes;
2. build/install the new binary;
3. validate configuration against the new version;
4. restart the service;
5. repeat known-public/known-private policy smoke tests.

Provider upgrades should likewise trigger at least provider-contract and publication-policy smoke tests before assuming compatibility.
