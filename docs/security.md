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

Root comparison must be segment-aware and normalized. String-prefix behavior such as treating `/media/archive-old` as beneath `/media/archive` is forbidden.

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

### Media type is independent of path

A matching path does not override type rules. Provider metadata must establish the asset's media type and the implementation must support that type.

Sidecars, project files and unknown provider asset types are never delivered as public media.

### Revalidate on delivery

A consumer reference is not an authorization record. Each delivery resolves current provider metadata and evaluates policy again.

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

Private, unknown and policy-denied assets should normally return the same `404` class of response. Avoid errors that reveal whether a private asset exists or why it failed policy.

## Consumer-compromise boundary

A consumer such as WordPress may be Internet-facing and therefore less trusted than the gateway/provider boundary.

WordPress should not hold the privileged Immich credential.

A compromised consumer may attempt to request arbitrary provider asset IDs. The expected result is:

```text
private asset ID -> gateway policy check -> 404
```

Any localhost-only search/discovery API exposed to a consumer must itself return only publication-eligible assets and must not become a generic private-library browser.

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

At minimum verify representative GPS-tagged phone and camera images do not expose sensitive EXIF/GPS through the selected provider preview.

If provider previews retain unsafe metadata, public image delivery must re-encode/strip metadata or remain blocked until a safe representation exists.

Do not assume that because a source file is in `public/` every embedded metadata field is intentionally public.

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
- provider timeout/failure -> bounded failure, no fallback;
- public request cannot select arbitrary URL/path/upstream;
- credential/config values do not appear in responses or logs under tested failures.

Fuzzing path-policy parsing is encouraged because this code is small and security-critical.

## Deployment hardening

The reference systemd service should run unprivileged with no required Linux capabilities and with standard hardening options where compatible.

The reference nginx configuration publishes only the delivery prefix and returns `404` for the rest of the hostname.

Host-specific deployment still requires operator review; examples are not a substitute for verifying actual firewall, tunnel and reverse-proxy configuration.
