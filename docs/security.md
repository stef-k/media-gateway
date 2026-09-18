---
title: "Security model"
---

# Security model

## Security goal

Media Gateway exists to make a narrow subset of media publicly deliverable **without making the private media provider or archive publicly reachable**.

The primary security property is:

> Knowing or controlling a consumer reference to a private provider asset must not be enough to make that asset public.

Publication permission is recomputed by the gateway from trusted provider metadata and configured policy.

## Assets to protect

- private images and videos indexed by the provider;
- provider API credentials;
- provider/NAS topology and private paths where disclosure is unnecessary;
- precise metadata that should not leave the private system, especially EXIF/GPS in public derivatives;
- the availability of the public site and private media provider.

## Threat model

Assume:

- the public hostname receives arbitrary hostile Internet traffic;
- attackers can guess, obtain or substitute provider asset IDs;
- WordPress or another consumer may contain a vulnerability or become compromised;
- malicious requests may attempt SSRF, traversal, resource exhaustion, header abuse or provider enumeration;
- operators can make configuration mistakes;
- the provider or NAS can become unavailable.

Media Gateway does not attempt to defend a fully compromised gateway host/root account from itself.

## Non-negotiable invariants

### Provider remains private

The public reverse proxy routes to Media Gateway, not Immich. There is no generic public route to the provider API, web UI or storage paths.

### Deny by default

An asset is public only when all required checks succeed. Missing/unknown/ambiguous metadata denies access.

### Allowed-root boundary

Policy evaluation begins by requiring the provider-indexed path to be beneath an explicitly configured allowed root such as:

```text
/media/archive
```

An Immich-managed upload under `/data/...` therefore cannot become public merely because one of its directory names is `public`.

Root comparison is component-aware. String-prefix behavior such as treating
`/media/archive-old` as beneath `/media/archive` is forbidden. Provider asset paths
must already be canonical absolute POSIX paths: empty/relative paths, dot
components, repeated separators, trailing separators, backslashes, control
characters, invalid UTF-8 and surrounding whitespace deny eligibility. `/` alone
is not an asset path. Evaluation never cleans paths, decodes URL escapes, performs
Unicode normalization or resolves filesystem links; provider paths are literal
metadata, not local filesystem locations.

### Exact publication segments

Policy v2 matches exact directory segments only:

```text
public
public-images
public-videos
```

Do not interpret these as prefixes. Names such as these must not match:

```text
public-old
public_backup
publicity
my-public
```

A segment grants eligibility only when it is a directory strictly below a
matched allowed root and above the asset basename. Matching text inside or above
the root, or only in the filename, grants nothing. If multiple publication
segments occur, at least one applicable global or root-scoped rule must permit
the media type. Rules combine with OR semantics, with no deny or precedence rules.
Named roots require unique logical names and canonical, unique, non-overlapping
paths other than `/`. Zero or multiple matching roots deny even for invalid
in-memory policy. Successful evaluation returns only logical root identity and
the root-relative parent collection; denial returns zero context. Trusted consumer
JSON exposes this logical context only after eligibility succeeds.

### Media type is independent of path

A matching path does not override type rules. Provider metadata must establish the asset's media type and the implementation must support that type.

The pure evaluator accepts only the exact normalized vocabulary `image` and
`video`; MIME strings, differently cased values and unknown types deny. It does
not infer type from the filename. Policy eligibility for video does not enable
video delivery; delivery support remains a separate mandatory check.

Sidecars, project files and unknown provider asset types are never delivered as public media.

### Revalidate on delivery

A consumer reference is not an authorization record. Each delivery resolves current
provider metadata and requires explicit boolean `isTrashed=false` and
`isOffline=false` before evaluating path/media policy. Either true denies even
when Immich retains an old eligible path. Missing, null or non-boolean lifecycle
fields are invalid metadata and fail closed. Lifecycle availability belongs to
the provider adapter, not the pure `publication.Evaluate` evaluator.

This is required for revocation by reorganizing the archive. Cached derivatives must not bypass this property unless a later explicit cache design provides equivalent bounded revocation behavior.

### No arbitrary upstream fetch

The service has one configured provider authority. Public and consumer requests cannot supply an upstream URL, host, scheme or filesystem path.

This is a hard SSRF boundary.

### No direct NAS reads

Media Gateway retrieves media through the provider API. The gateway receives no NAS mount and does not parse public requests into local filesystem access.

### No credential leakage

Provider credentials:

- live outside Git;
- use restrictive host permissions;
- never appear in public URLs;
- never appear in response bodies;
- are redacted from logs/errors;
- use the narrowest provider permission set that satisfies the implemented reads.

### Non-enumerating denial

Private, missing, invalid, unsupported, trashed, offline and policy-denied assets, including a
missing approved preview or original, return the same fixed `404` response. Provider auth,
transport, malformed metadata and representation validation failures return fixed
`502` responses. Neither class exposes provider details. HEAD emits no body.
Every handler response has `Cache-Control: no-store` and
`X-Content-Type-Options: nosniff`. Provider headers, cookies, filenames, cache
directives and conditional validators are never copied to the public response.

## Consumer-compromise boundary

A consumer such as WordPress may be Internet-facing and therefore less trusted than the gateway/provider boundary.

WordPress should not hold the privileged Immich credential.

A compromised consumer may attempt to request arbitrary provider asset IDs. The expected result is:

```text
private asset ID -> gateway policy check -> 404
```

The [private consumer API](consumer-api.md) returns only currently eligible image/video
assets and policy-derived collection identities. Every candidate passes current
lifecycle and exact publication evaluation. Collection asset selectors additionally
require exact logical root and parent-collection equality; descendants, near-prefix,
case and accent overmatches do not grant access. Detail reauthorizes independently.

Only the fixed safe projection leaves this boundary: logical context, path basename,
media type, nullable dimensions/duration in milliseconds, validated time strings,
always-present nullable coordinates and implemented non-null gateway capabilities.
Provider absolute paths, URLs, owner/library IDs and raw EXIF remain private.
There is no coordinate opt-in or `[consumer]` configuration table.

Gateway HMAC-SHA256 cursors bind query/policy; invalid signatures, versions, kinds
and cross-query tokens deny before provider I/O. A random ephemeral startup key
invalidates tokens on restart. Tokens contain no provider paths or credentials and
are never logged. Work is bounded to eight provider calls, 30 seconds and 512 KiB
consumer JSON; provider pages never exceed remaining output slots. Collection
identities can recur across pages; no persistent seen-set is maintained.

Only fixed structured Immich metadata search is used, never unpaginated folder
view. Provider search filters optimize candidate selection, not authorization.
The loopback TCP peer check ignores forwarded headers and grants no CORS access.
nginx must never expose `/internal/`, including through a local proxy.

## Request hardening

Authorization/confidentiality and availability are separate protections. Fresh
publication checks protect private media; intentionally published media remains
public. Valid-shaped UUID floods can still consume gateway/Immich metadata work,
and many long originals can occupy streams and provider I/O.

Existing abuse resistance includes strict Host/raw-route/GET/HEAD allowlists,
no public `/internal/`, tiny unused bodies, bounded header/body inactivity, fixed
gateway/provider authorities and bounded provider metadata/header work. nginx has
no retries, provider/storage fallback, buffering or cache. Established streams
retain cancellation, inactivity deadlines and bounded shutdown. The documented
single video-range disambiguation probe remains a bounded application exception;
there is no general provider retry or alternate representation fallback.

The reference nginx template adds an aggregate public-origin abuse envelope:
20 requests/second, burst 40 without delay, and 32 concurrent media requests.
Excess admission returns nginx-origin 429 before upstream work. Existing
route/method returns run before admission, so malformed noise retains its cheap
fixed denial. These lax reference values constrain gateway/provider workload and
concurrency; they are tunable starting points, not performance/security guarantees.

Limits are not authentication or authorization and do not make public media
private. They do not solve volumetric DDoS before traffic reaches the host/uplink.
An optional external edge can shed traffic before origin; no application limiter
or state store is needed. See [deployment tuning](deployment.md#availability-abuse-controls)
for aggregate limits and trustworthy optional client identity.

## Preview privacy and original source semantics

The public preview derivative must be tested for metadata leakage before production use.

Representative phone/camera/RAW preview privacy, human visual quality and live
revocation were validated against Immich 3.2.0. MIME/length checks alone do not
prove privacy or visual quality; repeat representative qualification after
provider/settings changes.

The preview adapter accepts only direct 200 JPEG/WebP responses of 1 byte through 16 MiB
with explicit length, no content encoding and no partial/transfer-coded response.
All redirects fail, including same-origin redirects to originals. No original or
fullsize request is made. A truncated/failed stream is aborted rather than marked
complete; bytes already streamed cannot be recalled. The gateway does not buffer
or inspect complete image contents.

If provider previews retain unsafe metadata, public preview delivery must re-encode/strip metadata or remain blocked until a safe representation exists.

Operators must account for original-source metadata when choosing publication conventions: `/original` exposes exact authorized source bytes, including EXIF/GPS, without inspection or rewriting. Use `/preview`, a separately qualified provider-generated web representation, when metadata-minimal delivery is required. RAW/HEIC source preservation does not imply browser display support.
Trusted catalogue coordinates do not change public preview routes or bytes,
embed GPS, add public metadata endpoints, or permit coordinates in logs. GPS is
deliberately exposed as nullable metadata only on the trusted eligible consumer
plane; public derivatives must remain metadata-minimal.

Original image GET/HEAD independently require current active metadata, exact policy and image type before fixed provider GET with no query. Only direct 200, one parameter-free `image/*` type and one explicit positive length are accepted, without content/transfer encoding or Content-Range. Source streaming has no arbitrary size ceiling and uses 60-second upstream/downstream per-I/O inactivity bounds. Provider header acquisition remains bounded independently of body reading. Image Range and all conditional headers never reach the provider; image original responses are full 200 with no Accept-Ranges.

Video originals use the same fixed endpoint and accept only parameter-free `video/*` or `application/mxf`. After current authorization, one bounded parsed byte range can reach the provider as canonical numeric syntax. Invalid/multiple ranges produce fixed 400; private/lifecycle/policy denials remain 404 before parsing. Validate provider 206 interval/total/length against the requested range, and provider 416 unsatisfiability/positive total before returning a zero-body 416. Only provider 404 for an authorized valid video Range triggers one fixed no-Range original GET without query/caller headers. Validate its normal video 200 headers: an unsatisfiable range against the positive full length closes the body and produces public zero-body 416. Only a suffix length greater than or equal to the total reuses that exact validated original body as full-representation 206, with constructed `Content-Range: bytes 0-(total-1)/total` and full length. Other satisfiable ranges close the body and produce sanitized 502. A second 404 remains public 404; probe auth/transport/status/framing failures become sanitized 502. The recovered suffix body retains exact-length/inactivity/cancellation enforcement; HEAD closes it unread. No third provider request is made. A provider 200 for Range is 502, never full-body or playback fallback. HEAD uses the identical provider GET and closes it. No arbitrary header forwarding or validator semantics are enabled. Source metadata remains part of authorized original bytes.

Both original kinds use fixed 60-second read/write inactivity deadlines, client cancellation and a 10-second shutdown drain; metadata/connect/header work remains hard-bounded. Stream failures abort without a plaintext suffix and emit at most one fixed warning; canceled clients stay quiet. Immich 3.2.0 validation covered poster privacy, original ranges and long streams. Revalidate relevant behavior when changing the provider or deployment.

## Logging

Logs should be useful for operations without becoming a private-media index.

Avoid logging:

- provider API keys;
- full private provider response bodies;
- private filesystem paths on routine denials;
- sensitive query/header values.

Safe operational fields can include request ID, status, bounded latency, variant, provider outcome class and a shortened/hashed asset correlation value when useful.

## Configuration safety

Startup validation should reject:

- empty or malformed named roots, duplicate names/paths, `/`, or overlapping paths;
- relative roots;
- duplicate segment/unordered-scope pairs, empty scopes or unknown/duplicate root references;
- obsolete `policy.allowed_roots` (sanitized migration error, no compatibility alias);
- unsupported configured media types;
- non-loopback listen addresses unless an explicit future option intentionally supports them;
- provider URLs with unsupported schemes;
- missing/unreadable credential files.

Security-sensitive defaults must be conservative.

## Security test coverage

At minimum automate cases for:

- asset inside allowed root + exact `public` + supported image -> allowed;
- asset inside allowed root without public segment -> denied;
- exact `public-images` image -> allowed;
- `public-images` video -> denied;
- exact `public-videos` image -> denied;
- `public-old`, `publicity`, `my-public` -> denied;
- asset outside allowed root with `public` segment -> denied;
- root string-prefix confusion -> denied;
- malformed provider path -> denied;
- unknown/unsupported type -> denied;
- arbitrary private asset ID supplied by caller -> denied;
- trashed/offline metadata with an otherwise eligible path -> 404, no preview fetch;
- missing/non-boolean lifecycle metadata -> bounded sanitized failure, no preview fetch;
- provider timeout/failure -> bounded failure, no fallback;
- public request cannot select arbitrary URL/path/upstream;
- credential/config values do not appear in responses or logs under tested failures.

Fuzzing path-policy parsing is encouraged because this code is small and security-critical.

## Deployment hardening

The reference systemd service should run unprivileged with no required Linux capabilities and with standard hardening options where compatible.

The reference nginx configuration proxies only canonical GET/HEAD
`/media/<UUIDv4>/preview` and `/media/<UUIDv4>/original` requests to fixed numeric loopback. An original-target
allowlist rejects encoded/normalized aliases; `/media` has an explicit denial
instead of nginx's automatic slash redirect. All private/unknown paths and
unknown Hosts fail closed without upstream access. This matters because nginx's
loopback peer would otherwise satisfy the private consumer API's peer check.
Caller headers/bodies are not forwarded, except Range on canonical original routes
and explicitly constructed transport context, which never authorizes publication. No cache, direct-storage
fallback, upload or WebSocket surface exists. Review inherited host configuration
and qualify the [ingress route matrix](deployment.md#nginx) before publishing.

Host-specific deployment still requires operator review; examples are not a substitute for verifying actual firewall, tunnel and reverse-proxy configuration.
