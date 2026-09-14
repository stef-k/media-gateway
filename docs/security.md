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

V0 does not attempt to defend a fully compromised gateway host/root account from itself.

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

V0 policy matches exact directory segments only:

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
segments occur, at least one exact rule must permit the media type. All allowed
roots share these rules: any qualifying root may grant eligibility, including
when roots are nested; their order does not affect the result.

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
the provider adapter, not the pure `publication.Eligible` evaluator.

This is required for revocation by reorganizing the archive. Cached derivatives must not bypass this property unless a later explicit cache design provides equivalent bounded revocation behavior.

### No arbitrary upstream fetch

The service has one configured provider authority. Public and consumer requests cannot supply an upstream URL, host, scheme or filesystem path.

This is a hard SSRF boundary.

### No direct NAS reads

V0 retrieves media through the provider API. The gateway receives no NAS mount and does not parse public requests into local filesystem access.

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
missing approved preview, return the same fixed `404` response. Provider auth,
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

The [private consumer API](consumer-api.md) returns only currently eligible images
from bounded candidate search or exact lookup. Every candidate passes the same
publication evaluator before any fields are serialized. The adapter omits
trashed/offline candidates before projection and rejects malformed lifecycle
metadata; an unavailable exact candidate receives the same fixed 404. Only IDs, dimensions,
capture/local times and relative preview paths leave this boundary by default.
The explicit `consumer.expose_coordinates=true` opt-in additionally permits only
a validated nullable latitude/longitude pair for currently eligible images. This
is deliberate disclosure to trusted consumers, which may apply their own public
UX/privacy decisions. Partial, nonnumeric, non-finite and out-of-range pairs fail
closed with sanitized provider errors. Raw EXIF and unrelated metadata remain
excluded; disabled mode neither requests nor decodes coordinates. The service
requires a loopback peer, ignores forwarded identity headers and gives no CORS
permission. nginx must never publish `/internal/`: a local proxy is still a local
peer, so application peer checks cannot establish public ingress isolation.

## Request hardening

The implementation should provide:

- explicit route shapes;
- `GET`/`HEAD` only on public media routes;
- bounded header/request sizes through Go/nginx defaults/configuration;
- provider client connection and request timeouts;
- no unbounded buffering of large upstream bodies;
- response content-type allow-listing;
- sensible concurrency/resource bounds if measurement shows they are needed;
- graceful cancellation when clients disconnect.

Do not add elaborate rate-limiting infrastructure before evidence. nginx/Cloudflare may provide coarse public abuse controls while application behavior remains bounded.

## Image privacy

The public representation must be tested for metadata leakage before production use.

Issue #17 implements streaming with synthetic HTTP contract tests. Issue #18
remains the external evidence gate: verify representative GPS-tagged phone and
camera images do not expose sensitive EXIF/GPS through the exact `size=preview`
derivative. MIME/length validation and source review do not prove image privacy,
content validity or visual quality. Production qualification remains blocked until
that real-provider evidence is accepted.

The adapter accepts only direct 200 JPEG/WebP responses of 1 byte through 16 MiB
with explicit length, no content encoding and no partial/transfer-coded response.
All redirects fail, including same-origin redirects to originals. No original or
fullsize request is made. A truncated/failed stream is aborted rather than marked
complete; bytes already streamed cannot be recalled. The gateway does not buffer
or inspect complete image contents.

If provider previews retain unsafe metadata, public image delivery must re-encode/strip metadata or remain blocked until a safe representation exists.

Do not assume that because a source file is in `public/` every embedded metadata field is intentionally public.
The consumer coordinate opt-in does not change public preview routes or bytes,
embed GPS, add public metadata endpoints, or permit coordinates in logs. GPS is
optional deliberately exposed publication metadata only on the trusted eligible
consumer plane; public derivatives must remain metadata-minimal.

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

- empty or malformed allowed roots;
- relative roots;
- duplicate/conflicting rules where semantics are ambiguous;
- unsupported configured media types;
- non-loopback listen addresses unless an explicit future option intentionally supports them;
- provider URLs with unsupported schemes;
- missing/unreadable credential files.

Security-sensitive defaults must be conservative.

## Security tests expected for V0

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

The reference nginx configuration publishes only the delivery prefix and returns `404` for the rest of the hostname.

Host-specific deployment still requires operator review; examples are not a substitute for verifying actual firewall, tunnel and reverse-proxy configuration.
