# Deployment

This document describes the reference deployment shape. The files under `deploy/` are templates, not unattended installers.

## Filesystem layout

The portable Linux service uses these regular files and directory, without extra ACL
grants. Root installs/administers them; the running process is unprivileged.

| Path | Owner:group | Mode | Purpose |
| --- | --- | --- | --- |
| `/usr/local/bin/media-gateway` | `root:root` | `0755` | Static executable; no setuid or file capabilities |
| `/etc/media-gateway` | `root:media-gateway` | `0750` | Service may traverse/read, never replace entries |
| `/etc/media-gateway/config.toml` | `root:media-gateway` | `0640` | Administrator-controlled startup policy |
| `/etc/media-gateway/immich.key` | `media-gateway:media-gateway` | `0400` | Separate owner-readable provider credential |
| `/etc/systemd/system/media-gateway.service` | `root:root` | `0644` | Administrator-controlled unit |

No runtime, data, cache, home or application log directory is required. The static
binary also uses ordinary OS resolver and CA trust files for DNS/HTTPS; those must
remain readable. Do not supply secrets or provider URLs through environment variables.
The application selects them only through the explicit TOML and key paths.

## Service account and installation

These are first-install commands from a reviewed checkout on a systemd Linux host
with `sudo`, shadow-utils (`groupadd`/`useradd`), coreutils and the reviewed
[Go toolchain](toolchain.md). Stop on any failure. Inspect existing names/paths first;
do not reuse an unrelated account, overwrite an existing installation, or alter
other services. Existing deployments should retain recoverable files before changes.

```sh
# Inspect first: absent entries are expected on a fresh host.
getent passwd media-gateway
getent group media-gateway

# Create once, with a dedicated primary group, locked password and no home/login.
# Use the distribution's installed nologin executable (commonly /usr/sbin/nologin).
command -v nologin
sudo groupadd --system media-gateway
sudo useradd --system --gid media-gateway --no-create-home \
  --home-dir /nonexistent --shell "$(command -v nologin)" media-gateway
sudo passwd --lock media-gateway
id media-gateway

# Build the static Linux amd64 application; no custom version injection.
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/media-gateway ./cmd/media-gateway
sudo install -o root -g root -m 0755 bin/media-gateway /usr/local/bin/media-gateway
sudo install -d -o root -g media-gateway -m 0750 /etc/media-gateway
sudo install -o root -g media-gateway -m 0640 \
  deploy/config.toml.example /etc/media-gateway/config.toml
sudoedit /etc/media-gateway/config.toml

# Provision the real key separately into a protected root-only source file outside
# the checkout. Replace this placeholder source path; never put the token in a
# command argument, environment variable, TOML or Git. Do not print its contents.
sudo install -o media-gateway -g media-gateway -m 0400 \
  /root/private-secrets/immich.key /etc/media-gateway/immich.key
sudo install -o root -g root -m 0644 \
  deploy/media-gateway.service /etc/systemd/system/media-gateway.service

# Inspect metadata only. Parent directories must also be administrator-controlled.
sudo stat -c '%U:%G %a %n' /usr/local/bin/media-gateway /etc/media-gateway \
  /etc/media-gateway/config.toml /etc/media-gateway/immich.key \
  /etc/systemd/system/media-gateway.service
sudo -u media-gateway test -r /etc/media-gateway/config.toml
sudo -u media-gateway test ! -w /etc/media-gateway/config.toml
sudo -u media-gateway test ! -w /etc/media-gateway
sudo -u media-gateway test -r /etc/media-gateway/immich.key
```

The account must have only its dedicated group, with no NAS, Immich, nginx or
administrative group memberships. Do not grant it file capabilities, ACL access to
archives, or storage mounts. Provider-visible `policy.allowed_roots` are metadata,
not local directories to create or mount.

`ProtectSystem=strict` prevents writes, **not reads** of otherwise accessible
storage. On a host that already mounts an archive, verify its permissions/ACLs deny
this identity traversal/read access. If storage is globally readable, the host
operator must isolate it (for example a reviewed host-specific `InaccessiblePaths=`
drop-in) before acceptance. The portable unit cannot name every host's storage path.
Do not change unrelated storage permissions blindly.

The key loader rejects any group/other permission, so `root:media-gateway 0640`
is not usable for the key. Service ownership with `0400` supplies owner-read;
the root-owned parent prevents replacement and the unit's read-only mount prevents
chmod/write by the owner while running. Root can rotate the file using the same
install command. Keep the protected provisioning source under the host's secret
management policy; never broaden permissions to troubleshoot startup.

## Configuration

Start from [`deploy/config.toml.example`](../deploy/config.toml.example) and follow
[the configuration contract](configuration.md). Adapt the numeric loopback listener,
private provider origin, provider-visible roots and literal publication segments.
Use an unprivileged port such as `2290`; the unit supplies no capability to bind
privileged ports on hosts that require one.

**TOML and key changes require service restart.** There is no hot reload or
validation-only command. Startup validates both before acquiring the listener;
invalid configuration/key or a bind failure exits nonzero and serves nothing.
`systemctl daemon-reload` rereads unit definitions only. A running process keeps
its previously loaded configuration/key until stopped, even if files change.
After policy changes and restart, check known-public success and known-private
denial. A configuration change is not proof of publication revocation until applied.

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

### Start, restart and stop

After installing the binary, configuration, key and unit, validate on the actual
host. A missing executable or an unsupported directive is a failure to resolve,
not a reason to remove hardening without review.

```sh
sudo systemd-analyze verify /etc/systemd/system/media-gateway.service
sudo systemctl daemon-reload
sudo systemctl enable --now media-gateway.service
sudo systemctl status --no-pager media-gateway.service
sudo journalctl -u media-gateway.service -n 30 --no-pager

# Apply TOML/key changes; unit edits additionally need verify + daemon-reload first.
sudo systemctl restart media-gateway.service
sudo systemctl status --no-pager media-gateway.service

# Deliberate stop sends SIGTERM and does not trigger Restart=on-failure.
sudo systemctl stop media-gateway.service
sudo systemctl show media-gateway.service -p ActiveState -p Result -p ExecMainStatus
```

`Type=simple` supervises the foreground process; a successful start command is not
application readiness. Confirm the `listening` journal event and actual loopback
socket. No provider probe or public health endpoint exists.

`Restart=on-failure` retries nonzero failures after five seconds; a clean signal
exit does not restart. Explicit `StartLimitIntervalSec=300s` and `StartLimitBurst=5`
stop repeated invalid-start retries independently of distribution defaults. After
fixing the cause, run `sudo systemctl reset-failed media-gateway.service` then
`sudo systemctl start media-gateway.service`. Do not reset repeatedly without
addressing the sanitized journal failure. The default SIGTERM/kill-control-group
behavior and final SIGKILL bound stop to 30 seconds around the 10-second Go drain.

### Sandbox and real-host qualification

Every directive is explained in the [unit](../deploy/media-gateway.service).
The static Go process has no JIT, child-process, device, clock, kernel administration
or filesystem-write requirement. Retain `MemoryDenyWriteExecute`, empty bounding
and ambient capabilities, `NoNewPrivileges`, SUID/SGID and personality restrictions,
and all existing home/kernel/cgroup protections. Private temporary directories
remain writable systemd exceptions but the application does not use them. Standard
resolver/CA reads remain available. IPv4/IPv6 and Unix sockets support provider,
DNS and same-host peers; address-family restrictions do not restrict destinations.
No private network namespace is used because host loopback/provider access is needed.

On a supported systemd host after PR review, record:

- exact binary revision, distribution/kernel/systemd version, successful unit verify,
  and no ignored/unsupported sandbox directives;
- process UID/GID and supplementary groups, zero effective/permitted/bounding/ambient
  capabilities, expected numeric loopback socket, and config/key metadata;
- successful startup and provider delivery with only documented file/network access,
  no writable data/cache directory, and denied access to actual archive mounts;
- journald lifecycle/failure visibility without secrets, clean SIGTERM/stop within
  30 seconds and no restart after clean stop;
- controlled failure/restart and persistent-invalid-start rate limiting, followed by
  correction/reset/start. Exercise destructive failure cases only in an isolated
  qualification instance, never against an unrelated production service.

Existing Go tests cover startup rejection, real process signals and bounded drain;
these do not prove systemd sandbox, service identity or host permission enforcement.
If privileges or a suitable host are unavailable, retain these exact evidence items
for M6 qualification (`stef-k/server-migration#10`) before #31 acceptance. Static
checks must not be represented as installed-host execution.

## nginx

nginx is the only public HTTP boundary. The gateway stays on numeric loopback,
conventionally `127.0.0.1:2290`; neither the edge nor nginx may target Immich/storage.
The [template](../deploy/nginx.conf) is included **once inside `http {}`**, with
its map/log format and two servers intact. Choose a free dedicated loopback origin
port (example `8089`), replace `media.example.com` and adapt log paths. The local
edge must send that Host. Unknown/missing Host selects the explicit default deny
server. Do not reuse an unrelated site's default listener or merge in its locations.

Only canonical `/media/<UUIDv4>/preview` GET/HEAD requests reach the gateway.
The raw request-target allowlist supplements nginx's normalized location matching;
publication/lifecycle authorization still belongs exclusively to the application.
Queries are ignored by the application and cannot select upstream behavior.

| Request to the configured Host | Expected ingress behavior |
| --- | --- |
| GET/HEAD `/media/<UUIDv4>/preview` | Fixed loopback gateway; current policy decides 200/404/502 |
| `/media`, `/media/`, extra segments or other representations | 404; no automatic slash redirect |
| `/internal/assets` or any `/internal/...` | 404 without upstream access |
| `/api`, `/api/...`, `/`, search/config/control/diagnostic/provider-looking or unknown paths | 404 without upstream access |
| Encoded path characters, dot segments, repeated slashes, traversal into/out of `/media/` | Denied before proxying; malformed HTTP may get nginx 400 |
| POST/PUT/PATCH/DELETE/OPTIONS on a preview route | 404 without upstream access; malformed/oversized requests may be rejected earlier |
| Unknown Host, even with a valid preview route | Default server denial without upstream access |

An exact `/media` location prevents nginx's implicit prefix slash redirect.
`^~ /media/` prevents regex-location takeover; the raw allowlist denies normalization
aliases. There is one fixed `proxy_pass` with no URI replacement or caller-selected
authority. Do not add rewrites, extra locations, inherited error-page redirects,
`alias`, `root`, `try_files`, storage fallback or forced cache behavior. Review the
**effective** `nginx -T` configuration for inherited directives from shared `http`
configuration, especially `error_page`, headers, authentication, cache and logging.
The template is not isolation from administrator-supplied nginx configuration.
See nginx's [location rules](https://nginx.org/en/docs/http/ngx_http_core_module.html#location)
and [proxy URI rules](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_pass).

The proxy connect/send limits are 5/10 seconds. Read inactivity is 70 seconds,
replacing the old 30 seconds so valid work can finish within the gateway's
60-second combined handler context and 65-second write deadline. nginx read/send
limits are **between I/O operations**, not total response deadlines; application
bounds still cap provider work. Client header/body inactivity is 5/10 seconds,
client send inactivity 65 seconds and keepalive 30 seconds. The edge must accommodate
these bounds rather than imposing a shorter unexplained origin deadline.

Responses stream with buffering/cache disabled and no temporary response files.
Application `Cache-Control: no-store` passes unchanged; the edge must honor it.
No retry, error interception, redirect rewriting or alternative upstream/storage
response is configured. nginx does not follow upstream redirects; the gateway
already rejects provider redirects. Connect/read failures normally yield 502/504;
a failure after headers truncates/closes the stream rather than replacing bytes.
Request bodies are unused, limited to 1 KiB and never forwarded. Caller headers
are replaced with a small transport-context allowlist, excluding credentials,
cookies and WebSocket upgrade. Forwarded fields never grant authorization; behind
a tunnel the recorded peer may be the tunnel unless separately reviewed trusted
real-IP handling is configured. No upload or WebSocket surface exists.

### Install and qualify ingress

Preserve existing host configuration before installing the adapted file in the
host's `http` include directory. Inspect `sudo nginx -T` locally (it may contain
unrelated sensitive configuration); validate before reload and preserve unrelated
services. Do not publish the configuration dump as evidence without review.

```sh
# Check the complete installed configuration before applying it.
sudo nginx -t
sudo systemctl reload nginx
# Inspect response headers through nginx with the configured Host and a known ID.
# Replace the placeholder; curl --path-as-is is essential for crafted-path checks.
curl --path-as-is -i -H 'Host: media.example.com' \
  http://127.0.0.1:8089/media/KNOWN-ELIGIBLE-UUID/preview
```

After PR source review, qualify the exact template revision on M6 against the
already-installed #31 service. Record nginx version, `nginx -t`, numeric loopback
sockets, eligible GET/HEAD, private/outside-root/near-match denial, every route/method
class above, no-store, access/error log operation and bounded upstream failure.
Confirm nginx targets Media Gateway only and responses/logs contain no actual
provider credentials or private metadata. Local nginx tests with a synthetic
upstream prove routing/transport only, not M6, real-provider policy or preview privacy.
The portable release/smoke bundle remains #33 work.

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

Lifecycle fields were re-verified on **2026-09-14** against the official
[Immich v3.2.0 response schema and mapper](https://github.com/immich-app/immich/blob/v3.2.0/server/src/dtos/asset-response.dto.ts).
`isTrashed` and `isOffline` are required booleans; the mapper derives trash state
from `deletedAt` and copies `isOffline`. The adapter requires both explicitly
false before returning metadata. Either true returns `ErrMissing`, even with an
old eligible path. Missing, null or non-boolean flags return `ErrMetadata`.
No lifecycle values are returned or logged.

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
status handling are unchanged. #18 recorded successful missing/private
requalification on accepted #24. After #27 merges, repeat the external-library
move/rescan test from its exact accepted head: without restarting the gateway,
the old trashed/offline ID must return fixed 404, as must a new ineligible ID.

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

Lifecycle/search behavior was re-verified on **2026-09-14** against the v3.2.0
[search service](https://github.com/immich-app/immich/blob/v3.2.0/server/src/services/search.service.ts),
[repository](https://github.com/immich-app/immich/blob/v3.2.0/server/src/repositories/search.repository.ts)
and [structured query builder](https://github.com/immich-app/immich/blob/v3.2.0/server/src/utils/database.ts).
The structured search used here does **not** automatically exclude trashed/offline
records. Its response uses the same asset mapper, so the adapter applies the
same required lifecycle booleans and omits unavailable items locally. The fixed
request remains unchanged; no extra lookups or page scans are added. Provider
count validation precedes filtering and the cursor is preserved, including on
empty pages. Exact unavailable lookups therefore return an empty page and a
consumer 404; a search-endpoint HTTP 404 remains an operational failure.

Every active item requires a UUIDv4 ID, nonempty unchanged `originalPath`, exact `IMAGE`
type (mapped to `image`), `width`, `height`, `fileCreatedAt` and `localDateTime`.
Dimensions use the schema's nullable nonnegative integer range through
9007199254740991: `null` means unknown, and zero is preserved. Times must parse
as RFC3339 with optional fractional seconds and are preserved as strings.
`fileCreatedAt` represents capture time; `localDateTime` retains the provider's
local wall-clock semantics and is not converted into another timezone. By default
no EXIF/GPS enters the candidate result; people and unrelated metadata never do.

Coordinate support was re-verified on **2026-09-14** against the exact v3.2.0 tag,
[commit `1b6098c9dbfffe978bec2d414606ed7a4c8e019a`](https://github.com/immich-app/immich/tree/1b6098c9dbfffe978bec2d414606ed7a4c8e019a).
The [search DTO](https://github.com/immich-app/immich/blob/1b6098c9dbfffe978bec2d414606ed7a4c8e019a/server/src/dtos/search.dto.ts)
and [service](https://github.com/immich-app/immich/blob/1b6098c9dbfffe978bec2d414606ed7a4c8e019a/server/src/services/search.service.ts)
pass the fixed `withExif` boolean to structured search.
The [query builder](https://github.com/immich-app/immich/blob/1b6098c9dbfffe978bec2d414606ed7a4c8e019a/server/src/utils/database.ts)
selects left-joined EXIF when enabled; there is no coordinate-only response selector.
The [asset mapper](https://github.com/immich-app/immich/blob/1b6098c9dbfffe978bec2d414606ed7a4c8e019a/server/src/dtos/asset-response.dto.ts)
omits `exifInfo` when unavailable, and the
[EXIF schema/mapper](https://github.com/immich-app/immich/blob/1b6098c9dbfffe978bec2d414606ed7a4c8e019a/server/src/dtos/exif.dto.ts)
provides nullable numeric `latitude` and `longitude`. This confirms #26's expected
request change: only `withExif=true` when configured. Full upstream EXIF still
counts toward the unchanged 1 MiB body bound, but the adapter decodes only the
coordinate pair and validates completeness, finiteness and geographic ranges.
Disabled mode keeps `withExif=false` and never decodes EXIF. Coordinates grant no
publication authority and never affect public preview retrieval.

The existing fixed origin, credential-safe transport, redirect rejection,
timeouts and cancellation apply. The entire JSON body, including ignored fields,
is capped at 1 MiB before decoding; excess candidates, invalid shapes, unsupported
media and malformed fields (including lifecycle flags) reject the whole page. Search uses the existing fixed
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
