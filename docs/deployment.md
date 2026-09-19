---
title: "Deployment"
---

# Deployment

This document describes the reference deployment shape. The files under `deploy/` are templates, not unattended installers.

## Operator path

For a first local run, start with the [configuration quick start](configuration.md#get-started).
For persistent installation, follow this sequence from a
[verified release bundle](release.md#download-and-verify-a-release):

1. Review [filesystem ownership](#filesystem-layout), then create the
   [service identity and install files](#service-account-and-installation).
2. Adapt [configuration and the protected credential](#configuration).
3. Validate and [start systemd](#start-restart-and-stop).
4. [Install and qualify nginx](#install-and-qualify-ingress), configure
   [HTTPS / edge routing](#https--edge) and check the [host firewall](#host-firewall).
5. Complete the [validation checklist](#validation-checklist) and
   [deployment smoke checks](release.md#portable-smoke-interface).
6. Use [logging](#logging) for operations and retain the
   [upgrade and rollback procedure](release.md#upgrade-and-rollback).

The [reviewed provider contracts](#provider-connectivity) below are reference
material for provider qualification and upgrades; the links above keep the
installation sequence accessible without moving that material.

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

First [download and verify a Linux amd64 GitHub Release](release.md#download-and-verify-a-release).
Run these first-install commands from its verified, extracted directory on a supported
Linux amd64 host with `sudo`, account-management tools (`getent`, `groupadd`,
`useradd`, `passwd`, `nologin`), coreutils, and systemd/nginx for the reference
integration. Go, Git and a source checkout are not target-host prerequisites.
Stop on any failure. Inspect existing names/paths first;
do not reuse an unrelated account, overwrite an existing installation, or alter
other services. Existing deployments should retain recoverable files before changes.

```sh
# The release guide must already have been used to verify this extracted directory.
./media-gateway -version

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

sudo install -o root -g root -m 0755 \
  ./media-gateway /usr/local/bin/media-gateway
sudo install -d -o root -g media-gateway -m 0750 /etc/media-gateway
sudo install -o root -g media-gateway -m 0640 \
  ./config.toml.example /etc/media-gateway/config.toml
sudoedit /etc/media-gateway/config.toml

# Provision the real key separately into a protected root-only source file outside
# the release directory. Replace this placeholder source path; never put the token in a
# command argument, environment variable, TOML or Git. Do not print its contents.
sudo install -o media-gateway -g media-gateway -m 0400 \
  /root/private-secrets/immich.key /etc/media-gateway/immich.key
sudo install -o root -g root -m 0644 \
  ./media-gateway.service /etc/systemd/system/media-gateway.service

# Inspect metadata only. Parent directories must also be administrator-controlled.
sudo stat -c '%U:%G %a %n' /usr/local/bin/media-gateway /etc/media-gateway \
  /etc/media-gateway/config.toml /etc/media-gateway/immich.key \
  /etc/systemd/system/media-gateway.service
sudo -u media-gateway test -r /etc/media-gateway/config.toml
sudo -u media-gateway test ! -w /etc/media-gateway/config.toml
sudo -u media-gateway test ! -w /etc/media-gateway
sudo -u media-gateway test -r /etc/media-gateway/immich.key
```

Developers/maintainers intentionally building from source should use the
[toolchain and development guidance](toolchain.md#build-and-ci-baseline) and
[qualification bundle procedure](release.md#build-and-verify-qualification-bundles).

The account must have only its dedicated group, with no NAS, Immich, nginx or
administrative group memberships. Do not grant it file capabilities, ACL access to
archives, or storage mounts. Provider-visible `policy.roots` are metadata,
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

Start from [`deploy/config.toml.example`](https://github.com/stef-k/media-gateway/blob/main/deploy/config.toml.example) and follow
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

Only image/video `GET`/`HEAD /media/<asset-id>/preview` and `/media/<asset-id>/original` can deliver media. `/health`, search,
public metadata/control routes and unsupported methods remain fixed denials.
The [public catalog API](catalog-api.md) exposes `/catalog/collections`, `/catalog/assets`
and asset detail routes through nginx with credential-free CORS. There is
no startup provider probe: `listening` means the loopback listener was acquired,
not that previews are qualified or available. HTTP limits are 5 seconds for
headers, 10 seconds for request reads, 65 seconds for response writes, 30 seconds
for idle connections, and 16 KiB for headers (plus Go's parsing allowance). No
handler reads request bodies. A 60-second handler context bounds authorization/preview
work. Metadata/search/preview retain their configured timeout through body reading.
Original connect/header acquisition remains bounded, while established image/video
original bodies use 60-second read/write inactivity deadlines refreshed per I/O.
The 65-second write default remains for ordinary responses; active original streams
can continue beyond it. Stalled upstream/downstream I/O remains finite.
Client disconnects cancel upstream work. Shutdown retains the 10-second drain.

Preview bodies must have a known positive length of at most 16 MiB and an exact
JPEG/WebP content type. Headers are validated before streaming; a short body or
mid-stream failure aborts the response. HEAD checks current metadata/policy and
preview headers, then closes the body. All responses use `no-store`; configure
nginx/edge to honor it and never force-cache these routes. Private/missing/invalid
assets get `404`; provider/auth/validation failures before headers get `502`.
Real Immich 3.2.0 validation covered preview privacy/quality and lifecycle revocation;
provider/settings upgrades require renewed representative checks.

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

Every directive is explained in the [unit](https://github.com/stef-k/media-gateway/blob/main/deploy/media-gateway.service).
The static Go process has no JIT, child-process, device, clock, kernel administration
or filesystem-write requirement. Retain `MemoryDenyWriteExecute`, empty bounding
and ambient capabilities, `NoNewPrivileges`, SUID/SGID and personality restrictions,
and all existing home/kernel/cgroup protections. Private temporary directories
remain writable systemd exceptions but the application does not use them. Standard
resolver/CA reads remain available. IPv4/IPv6 and Unix sockets support provider,
DNS and same-host peers; address-family restrictions do not restrict destinations.
No private network namespace is used because host loopback/provider access is needed.

On a supported systemd host, record:

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
for qualification on the target host. Static
checks must not be represented as installed-host execution.

## nginx

nginx is the only public HTTP boundary. The gateway stays on numeric loopback,
conventionally `127.0.0.1:2290`; neither the edge nor nginx may target Immich/storage.
The [template](https://github.com/stef-k/media-gateway/blob/main/deploy/nginx.conf) is included **once inside `http {}`**, with
its map/log format and two servers intact. Choose a free dedicated loopback origin
port (example `8089`), replace `media.example.com` and adapt log paths. The local
edge must send that Host. Unknown/missing Host selects the explicit default deny
server. Do not reuse an unrelated site's default listener or merge in its locations.

Only canonical media GET/HEAD and catalog GET routes reach the gateway.
The raw request-target allowlist supplements nginx's normalized location matching;
publication/lifecycle authorization still belongs exclusively to the application.
Media queries are ignored. Catalog queries have a bounded strict selector allowlist;
no query can select arbitrary upstream behavior.

| Request to the configured Host | Expected ingress behavior |
| --- | --- |
| GET/HEAD `/media/<UUIDv4>/preview` or `/media/<UUIDv4>/original` | nginx admission may return 429; otherwise fixed loopback gateway and current policy/range validation decide 200/206/400/404/416/502 |
| `/media`, `/media/`, extra segments or other representations | 404; no automatic slash redirect |
| GET `/catalog/collections`, `/catalog/assets`, `/catalog/assets/<UUIDv4>` | Shared admission may return 429; application validates selectors, policy and bounded JSON |
| Other catalog paths, catalog HEAD/OPTIONS or legacy `/internal/` paths | 404 without upstream access |
| `/api`, `/api/...`, `/`, search/config/control/diagnostic/provider-looking or unknown paths | 404 without upstream access |
| Encoded path characters, dot segments, repeated slashes, traversal into/out of `/media/` | Denied before proxying; malformed HTTP may get nginx 400 |
| POST/PUT/PATCH/DELETE/OPTIONS on either media route | 404 without upstream access; malformed/oversized requests may be rejected earlier |
| Unknown Host, even with a valid media route | Default server denial without upstream access |

An exact `/media` location prevents nginx's implicit prefix slash redirect.
`^~ /media/` prevents regex-location takeover; the raw allowlist denies normalization
aliases. Both catalog and media locations use the same fixed upstream with no URI
replacement or caller-selected authority. Do not add rewrites, extra locations, inherited error-page redirects,
`alias`, `root`, `try_files`, storage fallback or forced cache behavior. Review the
**effective** `nginx -T` configuration for inherited directives from shared `http`
configuration, especially `error_page`, headers, authentication, cache and logging.
The template is not isolation from administrator-supplied nginx configuration.
See nginx's [location rules](https://nginx.org/en/docs/http/ngx_http_core_module.html#location)
and [proxy URI rules](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_pass).

The proxy connect/send limits are 5/10 seconds. Catalog read inactivity is 35 seconds
for its 30-second application deadline. Media read inactivity is 70 seconds,
accommodating the gateway's 60-second original read/write inactivity bounds. nginx read/send
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
cookies, If-Range, If-None-Match, If-Modified-Since and WebSocket upgrade. Only
canonical original routes forward caller Range to the gateway; preview strips it.
The application validates video Range after authorization and ignores image Range. Forwarded fields never grant authorization; behind
a tunnel the recorded peer may be the tunnel unless separately reviewed trusted
real-IP handling is configured. No upload or WebSocket surface exists.

Run `python3 scripts/test_nginx.py` with local nginx installed to exercise both
canonical catalog/media routes, malformed/private/provider denials, header stripping and
route log classification in an isolated process. A skipped test is unavailable
local nginx evidence, not a pass. This synthetic upstream does not qualify a deployed provider.

### Availability-abuse controls

The shipped aggregate limits share one bucket per `$server_name`, across both
media representations and GET/HEAD. They work even when a local tunnel/reverse
proxy makes every visitor appear to have the same peer address. Include the zones
once in `http {}`; the template applies admission only in the public media
location after the existing return-only route/method checks:

```nginx
# http context: shared aggregate service budgets, independent of client address.
limit_req_zone $server_name zone=media_gateway_rate:1m rate=20r/s;
limit_conn_zone $server_name zone=media_gateway_conn:1m;

# Canonical media location: reject excess admission before gateway work.
limit_req_status 429;
limit_conn_status 429;
limit_req zone=media_gateway_rate burst=40 nodelay;
limit_conn media_gateway_conn 32;
```

This allows 20 requests/second with burst 40 and no queueing delay, plus 32
concurrent catalog/media requests. Both limits return **429 Too Many Requests** from
nginx; application response contracts remain unchanged. Connection accounting
starts after complete request headers while requests are being processed, not
for every idle TCP socket. HTTP/2 and HTTP/3 count concurrent requests separately.
Wrong/missing Host, malformed paths, unsupported methods and private/unknown routes
retain their existing cheap denials before admission and upstream access.

These intentionally lax reference values are not security/performance guarantees.
Tune rate, burst and concurrency together against measured traffic, gateway/provider
capacity and long-stream occupancy; low values can reject legitimate visitors.
No response `limit_rate` is enabled: slowing large media can prolong resource
occupancy. The controls do not make published media private or stop volumetric
traffic before it reaches the host/uplink.

For optional observation-first tuning, temporarily add in the media location:

```nginx
# Observe excess admission without enforcing either limit during measurement.
limit_req_dry_run on;
limit_conn_dry_run on;
```

Remove these overrides to restore enforcement; dry-run is not the shipped default.
The route-classified access log includes `$limit_req_status` and
`$limit_conn_status` as `request_limit` and `connection_limit`. Observe `REJECTED`
and `REJECTED_DRY_RUN` alongside status, latency and provider capacity; a limiter
that was not reached can have no status. No raw paths, queries or headers are added.
Both limit-event log levels are `notice`, below the existing `warn` error-log
threshold, keeping ordinary shedding in access logs without an error-log flood.
Retain normal host rotation and privacy rules. Validate the effective configuration
with `nginx -t` before applying an operator change.

Official directive references:
[request limiting](https://nginx.org/en/docs/http/ngx_http_limit_req_module.html),
[connection limiting](https://nginx.org/en/docs/http/ngx_http_limit_conn_module.html).
The isolated nginx regression queues a valid burst without changing these values,
checks 429/non-forwarding and holds 32 synthetic upstream responses to exercise
concurrency rejection. It also verifies cheap denials under load.

### Optional per-client limits

Directly Internet-facing nginx normally sees the network peer in `$remote_addr`.
Behind a tunnel/reverse proxy it may see only the edge peer: blindly using per-IP
limits there can put every visitor into one bucket. Only with a trustworthy client
address, optionally add separate zones and limits alongside the aggregate ones:

```nginx
# http context: illustrative per-client values; tune for shared NATs and capacity.
limit_req_zone $binary_remote_addr zone=media_client_rate:1m rate=10r/s;
limit_conn_zone $binary_remote_addr zone=media_client_conn:1m;

# Same canonical media location, retaining both aggregate limit directives.
limit_req zone=media_client_rate burst=20 nodelay;
limit_conn media_client_conn 8;
```

Never trust arbitrary `X-Forwarded-For`. Use
[`set_real_ip_from`, `real_ip_header` and `real_ip_recursive`](https://nginx.org/en/docs/http/ngx_http_realip_module.html)
only after explicitly trusting proxy addresses and their header semantics,
including how the chain is constructed and which peers can reach the origin.
Do not use a universal trusted-address range or assume every local proxy supplies
verified identity. Alternatively, enforce per-client limits at an external edge
where client identity is already known. Edge limiting can reject unwanted traffic
before it consumes origin/uplink capacity; no particular edge provider is required.

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

For the target host or changed integration, record nginx version, `nginx -t`, numeric loopback
sockets, eligible GET/HEAD, private/outside-root/near-match denial, every route/method
class above, no-store, access/error log operation and bounded upstream failure.
Confirm nginx targets Media Gateway only and responses/logs contain no actual
provider credentials or private metadata. Local nginx tests with a synthetic
upstream prove routing/transport only, not installed-host behavior, real-provider policy or preview privacy.
Use the [release and portable smoke procedure](release.md) for bundle
validation before production cutover.

## Logging

Logging is intentionally split between nginx and the Go service. See [`docs/logging.md`](logging.md) for the authoritative privacy/severity rules.

### nginx

nginx owns the public HTTP access/error record for the published media route. It is the right place to observe client/method/status/bytes/timing and reverse-proxy transport failures.

Do not put provider credentials or authorization headers into access logs. If a future feature introduces signed/tokenized query parameters, review the nginx log format before enabling it so signatures/tokens are not persisted accidentally.

### Media Gateway

The Go process uses the standard library `log/slog` for lifecycle, sanitized startup/configuration state, provider/runtime failures and security-relevant diagnostics. It writes to stderr/stdout; under systemd, journald owns persistence and rotation.

There is no application-managed logfile or rotation subsystem and no second full access log duplicating nginx.

Useful operator commands include:

```bash
journalctl -u media-gateway
journalctl -u media-gateway -f
```

Routine denied/malformed Internet traffic must not become an unbounded high-severity application-log flood. Application logs must not contain provider API keys, authorization headers, private provider/NAS paths, GPS/EXIF metadata or provider error bodies.

## HTTPS / edge

Use an operator-controlled public hostname, for example:

```text
https://media.example.com
```

Public HTTPS can terminate at nginx or an optional operator-controlled edge
(such as Cloudflare or another HTTPS reverse proxy). No external edge or tunnel
service is required. The edge must route only to the nginx Media Gateway vhost; it must not route directly to Immich.

If you choose Cloudflare Tunnel, point the hostname at the dedicated nginx origin listener. Do not create a tunnel route to the Immich application port.

## Provider connectivity

A local Immich deployment may be configured as:

```text
http://127.0.0.1:2283
```

Other private-network layouts are valid, but the provider URL is operator configuration and cannot be supplied by public requests.

### Reviewed metadata contract

Verified on 2026-09-12 against official Immich OpenAPI version **3.2.0** at
[the reviewed upstream OpenAPI snapshot](https://github.com/immich-app/immich/blob/0f901eea5ec2d3ebf85188b8c1dd193ae3619966/open-api/immich-openapi-specs.json).
The [Retrieve an asset operation](https://api.immich.app/endpoints/assets/getAssetInfo)
(`getAssetInfo`) uses `GET /api/assets/{id}` and returns HTTP `200` with
`application/json`. The API server prefix is `/api`; configured `base_url`
remains an origin, not an API path.

Use a non-administrator account with access to the intended assets and an API key
restricted to **`asset.read` + `asset.view` + `asset.download`**, sent only in the **`x-api-key`** header. This is the
union required by metadata/search (`asset.read`), preview (`asset.view`) and
original (`asset.download`) operations; it does not override Immich's asset-access checks.
No administrator or write permission is required. Candidate
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
`publication.Evaluate`; neither ID knowledge nor successful metadata retrieval
grants publication. Video mapping alone never authorizes delivery; current publication policy must also succeed.

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
and other failures to fixed 502 responses. Only the approved catalog projection is public; raw provider metadata remains private.

Immich v3.2.0 metadata lookup deliberately returns HTTP 400 for missing assets
or absent `asset.read` access: see [the access gate](https://github.com/immich-app/immich/blob/v3.2.0/server/src/utils/access.ts)
and [asset retrieval](https://github.com/immich-app/immich/blob/v3.2.0/server/src/services/asset.service.ts).
For this fixed UUIDv4-prevalidated endpoint only, `Asset()` maps 400 alongside 404
to `ErrMissing` without reading provider error bodies. Preview and candidate-search
status handling use their own endpoint contracts. Deployed Immich 3.2.0 checks
confirmed missing/private denial and external-library move/rescan revocation
without restarting the gateway: the old trashed/offline ID
and new ineligible ID returned fixed 404.

This verification is an upstream API/source review plus local HTTP contract tests,
not qualification against a deployed Immich instance. Re-check API permissions
and run provider/policy smoke tests when upgrading Immich.

### Reviewed candidate search contract

Re-verified on **2026-09-15** against Immich **v3.2.1**, with the unchanged
v3.2.x structured-search/asset schema. Deployed evidence uses v3.2.0;
source review does not establish deployed behavior on v3.2.1. The optional boolean
`withExif` search schema was rechecked against both versions on 2026-09-19 for
the source-metadata privacy guard.

Use only `POST /api/search/metadata`, `x-api-key`, permission **asset.read**, and
HTTP 200 `application/json`. The [search DTO](https://github.com/immich-app/immich/blob/v3.2.1/server/src/dtos/search.dto.ts),
[query builder](https://github.com/immich-app/immich/blob/v3.2.1/server/src/utils/database.ts),
[search repository](https://github.com/immich-app/immich/blob/v3.2.1/server/src/repositories/search.repository.ts)
and [asset mapper/schema](https://github.com/immich-app/immich/blob/v3.2.1/server/src/dtos/asset-response.dto.ts)
define the reviewed request and response contract.

Gateway-generated filters always include `type.in` with eligible IMAGE/VIDEO media,
`isOffline.eq=false` and `trashedAt.eq=null`. Discovery adds a configured root
`originalPath.startsWith` and segment-shaped `like` filter; collection assets add
a prefix resolved from the configured root plus validated relative selector.
SQL wildcard characters in these literals are escaped. Immich wraps `like` with
wildcards itself; both path operators are case/accent-insensitive, so every
candidate still requires exact lifecycle/path/media/publication policy evaluation.
An exact UUIDv4 detail adds `id.eq`; no caller filter tree or provider URL is accepted.

Ordering is fixed `fileCreatedAt desc` with the provider's ID tie-break. Offset
cursors are not a changing-library snapshot. Provider pages are 1–100 candidates,
never larger than remaining consumer slots. Up to eight calls fill one consumer
request, under a 30-second overall deadline plus the configured per-call timeout.
No retries occur. Gateway-signed consumer cursors wrap bounded internal provider
continuation; callers cannot submit raw provider cursors.

`withPeople=false` and `withStacked=false` are fixed. Discovery requests
`withExif=false` and decodes only policy facts, ignoring unrelated malformed
projection fields. Asset lists/details set `withExif` from
`privacy.expose_source_metadata` (default false). They always require explicit
nullable nonnegative safe-integer width/height/duration. Only when enabled do
they validate RFC3339 capture/local time strings and a nullable coordinate pair;
otherwise hidden sensitive metadata is ignored and projects as null.
Immich duration is integer **milliseconds**, exposed as `duration_ms`. Local wall
time strings are preserved without timezone conversion. Filename comes from the
validated path basename, never arbitrary provider filename metadata.

The adapter requires explicit active lifecycle booleans independently of filters.
Unavailable candidates are omitted. Missing/null/invalid lifecycle fields or
malformed consumed projection metadata fail the request. Full JSON, including
ignored EXIF, is bounded to 1 MiB. The response must include `assets.items`, matching
`assets.count`, and `assets.nextCursor` as a nonempty bounded string or explicit null.
Internal provider cursors are valid UTF-8, at most 1024 bytes, without controls.
No provider cursor, path, coordinates, raw body or credential is logged.

Collection discovery is at least once, with only in-page identity deduplication.
No folder-view endpoint is used: those endpoints are unpaginated and restricted
to timeline visibility. There is no persistent catalog/cursor state. See the
[consumer API](catalog-api.md) for exact selectors, signatures, capabilities and
failure semantics. Public preview behavior and permissions remain unchanged.

### Reviewed preview contract

Re-verified on 2026-09-12 against the same official Immich 3.2.0 source snapshot. The OpenAPI `viewAsset` operation is
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
JPEG/WebP previews. Media Gateway accepts only exact `image/jpeg` and `image/webp`,
not the generic OpenAPI body type. The 16 MiB bound is a gateway limit, not an
Immich guarantee. Missing previews deny; redirects and invalid representation
headers fail with 502. No retry, alternate representation or storage fallback exists.

Real Immich 3.2.0 validation covered representative phone/camera/RAW quality,
metadata/privacy, direct responses and live revocation through this route.
Repeat representative checks after provider versions/settings change. The
gateway itself does not strip image metadata.

### Reviewed original image contract

Exact original delivery requires `privacy.expose_source_metadata=true`. The
default returns fixed 404 without provider original requests; previews/posters
remain available. Enabling deliberately exposes exact metadata-bearing source
bytes. Use explicit opt-in for the original checks below.

Reverified on 2026-09-15 against official Immich **v3.2.1**:
[controller](https://github.com/immich-app/immich/blob/v3.2.1/server/src/controllers/asset-media.controller.ts),
[download DTO](https://github.com/immich-app/immich/blob/v3.2.1/server/src/dtos/asset.dto.ts),
[service](https://github.com/immich-app/immich/blob/v3.2.1/server/src/services/asset-media.service.ts),
[file response](https://github.com/immich-app/immich/blob/v3.2.1/server/src/utils/file.ts)
and [MIME mappings](https://github.com/immich-app/immich/blob/v3.2.1/server/src/utils/mime-types.ts).
Deployed validation used v3.2.0; source review does not prove an upgrade.

The sole operation is GET `/api/assets/<UUIDv4>/original`, with `x-api-key` and
`asset.download`. No provider query is sent: optional `edited` defaults false, so
this selects source `originalPath`, not edited media. Caller Range/conditional
headers are never forwarded. Public HEAD also opens provider GET, validates its
status/headers, then closes the body immediately; no provider HEAD support is assumed.

Require direct HTTP 200, exactly one valid parameter-free `image/*` Content-Type,
and exactly one explicit positive int64 Content-Length. Reject all transfer/content
encoding and Content-Range. There is no arbitrary original size ceiling. RAW/HEIC
are valid source types without conversion, even if a browser cannot display them.
GET streams the declared length; truncation aborts the response without a plaintext
suffix. Missing original is fixed 404; auth, redirects, other status and malformed
responses are sanitized 502 before public headers. No alternate fetch exists.

The original transport uses fixed authority and credential, no environment proxy,
no redirects or compression, and a fresh HTTP/1 connection. It checks bounded wire
headers before Go normalizes duplicate lengths/transfer fields. Dial/TLS are bounded
by the smaller of five seconds and `provider.request_timeout`; response headers by
the configured timeout. The HTTP client has no absolute body timeout. Established
original streams use fixed 60-second read/write inactivity deadlines.
Ordinary responses retain the finite server defaults; nginx read/send inactivity
remains 70/65 seconds. Client disconnect and bounded shutdown cancel originals.

Public success constructs only Content-Type, Content-Length, no-store and nosniff
(plus standard HTTP date/framing). Provider disposition, filename, cache validators,
cookies and other headers are discarded. `/preview` is a separately qualified
provider-generated web representation; `/original` is exact authorized source bytes.
Original does **not** strip EXIF/GPS or inspect embedded metadata.

The current schema uses `privacy.expose_source_metadata` to permit exact originals;
preview remains available under current authorization. The dedicated key requires
`asset.read`, `asset.view` and `asset.download`, without write/admin permissions. See [original image qualification](release.md#original-image-qualification)
for source-identity and privacy checks.

### Reviewed video contract

Reverified on 2026-09-15 against Immich **v3.2.0**, the deployed validation version:
[controller](https://github.com/immich-app/immich/blob/v3.2.0/server/src/controllers/asset-media.controller.ts),
[service](https://github.com/immich-app/immich/blob/v3.2.0/server/src/services/asset-media.service.ts),
[file helper](https://github.com/immich-app/immich/blob/v3.2.0/server/src/utils/file.ts),
and [MIME types](https://github.com/immich-app/immich/blob/v3.2.0/server/src/utils/mime-types.ts).

Video poster uses only GET thumbnail `size=preview`, with the existing direct-200
JPEG/WebP, positive-length <=16 MiB checks. Video original uses only GET original
without query; `/video/playback` can select encoded media and is never a fallback.
The dedicated key union stays `asset.read + asset.view + asset.download`.
Original type is parameter-free `video/*` or the explicit `application/mxf` exception.

Only an authorized video original parses Range: one field value <=128 bytes,
unsigned signed-int64 decimal explicit/open/suffix syntax, no whitespace or lists.
Malformed ranges return fixed 400 without opening original. Private/lifecycle/policy
denials stay 404 before parsing. Canonical Range is the only caller-derived header
sent to Immich; all conditionals are discarded. No Range requires direct 200;
a satisfiable Range requires 206 with mathematically exact Content-Range and length.
Provider 200 for Range is 502. A direct valid unsatisfiable 416 requires `bytes */total`
with positive total and produces a zero-body public 416. HEAD uses the same provider GET
and closes the body. Video originals advertise Accept-Ranges; image originals remain
full 200 and ignore Range. All public range headers are validated and constructed.

Deployed-provider evidence on 2026-09-18 showed Immich 3.2.0 returning 404 for an
unsatisfiable original Range, consistent with its file-send error wrapper. Only
this authorized valid-range 404 triggers one fixed no-Range original GET, without
query or caller headers. Its normal video 200 framing supplies the positive full
length. Unsatisfiable math closes the body and produces public zero-body 416.
Deployed Immich 3.2.0 also returns 404 for an oversized suffix: a suffix length
greater than or equal to the total reuses this validated no-Range original body
as full-representation 206, with `Content-Range: bytes 0-(total-1)/total` and full
length. HEAD returns identical framing and closes the body without reading it.
Other satisfiable math closes the body and produces sanitized 502; no third
provider request is made. A second
404 remains public 404; probe auth/transport/status/framing failures produce 502.
Normal 206 performs no probe. No loops or playback substitution are introduced.

Use the [video worksheet](video-qualification.md) to validate a deployment in
isolation. Remove temporary services and credential copies after validation.

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
8. preview has the expected type and qualified metadata privacy; original preserves exact authorized source bytes, including embedded metadata;
9. provider outage produces bounded failure and no storage fallback;
10. secrets/private provider paths do not appear in nginx or journald logs or responses;
11. nginx access logging records the intended public request facts without secret query/header material;
12. gateway lifecycle/provider failures are visible through `journalctl -u media-gateway`;
13. repeated denied requests do not create an obvious unbounded high-severity application-log flood;
14. service survives/restarts cleanly under systemd;
15. unrelated host services remain healthy.

## Upgrades

Follow the [bundle install/upgrade/rollback procedure](release.md). Preserve the
previous binary, protected configuration/key and adapted templates before replacing
anything. TOML/key changes require restart; there is no hot reload. Provider
upgrades also require representative provider-contract, policy and privacy checks.
