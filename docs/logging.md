---
title: "Logging"
---

# Logging

Media Gateway uses two complementary logging layers. They serve different purposes and neither replaces the other.

## nginx / edge logging

nginx owns public HTTP access and transport logging for the published Media Gateway route.

This layer is useful for:

- client address and request method;
- public URI/path;
- HTTP status;
- response size;
- request/proxy timing;
- nginx/upstream transport failures.

The nginx access log is the primary request/access record. Media Gateway should not duplicate a second full access log for every successful request. Log retention/rotation for these files belongs to the host's normal nginx/logrotate policy rather than Media Gateway.

Public-edge logging must not record secrets. In particular:

- never log `Authorization` or provider credentials;
- avoid logging query strings if a future feature places signatures/tokens in them;
- do not expose provider/NAS paths through public URLs in the first place;
- review any custom nginx log format before enabling it on an Internet-facing deployment.

The public media surface is intentionally path-based and requires no secret query parameter.
The reference `media_gateway` access format records the peer, method, classified
route (`preview`, `original` or `denied`), status, bytes and request/upstream timings. It omits
raw paths, asset IDs, query strings, Host, Referer, User-Agent and all credential
headers. Both the default-deny and media servers use this format and dedicated
access/error files. Behind an edge the peer may be the local tunnel; do not trust
caller-supplied forwarded chains as identity.

nginx error logging at `warn` retains proxy transport diagnostics and can include
caller-supplied request lines/queries. It is not a configurable access-log format:
query-safe access logging alone does **not** qualify future signed/query-token URLs.
Review error logs and every edge layer before introducing secrets in URLs; never
put provider credentials there. The only configured upstream is the gateway, so
actual provider/NAS paths and provider credentials are unavailable to nginx.
Attacker-supplied text in an error request line is not trusted provider metadata.
Do not enable debug/header logging. Check installed log permissions and host
rotation/retention, including inherited logging destinations, during qualification.

## Media Gateway application logging

The Go process owns application/security/diagnostic logging that nginx cannot provide.

Use the Go standard library `log/slog` and write to stderr/stdout. Under the reference systemd deployment, journald owns persistence, indexing and rotation. The application does not create or rotate its own log files.

Log useful lifecycle and failure events such as:

- process start/stop and build/version metadata;
- successful configuration load at a sanitized summary level;
- startup/configuration validation failures;
- provider connectivity/request failures and timeouts;
- unexpected provider status/content-type/size failures;
- internal errors;
- security-relevant denial categories when useful for diagnosis.

Do not routinely log:

- provider API keys or secret-file contents;
- authorization headers;
- full private provider/NAS paths;
- private metadata such as GPS or EXIF;
- response bodies from provider failures;
- full per-request success records already covered by nginx access logs.

Known-private or malformed public requests are normal hostile/invalid Internet traffic and must not become an unbounded warning-log amplification path. Routine policy denials should normally remain quiet or low-verbosity; unexpected invariant/provider failures merit higher severity.

## Service events

The service emits JSON `slog` records to stderr at INFO for `starting`,
`configuration loaded`, `listening`, `stopping`, and `stopped`. Startup metadata is
limited to build provenance and the validated loopback listener. It never dumps
configuration, roots, provider URLs or credential paths. Configuration errors use
the shared loader's sanitized field/operation messages; bind/serve/shutdown errors
use fixed failure descriptions and cause a nonzero exit.

The public handler keeps successes and routine malformed/private/missing denials
quiet. Exceptional failures before headers emit one `provider request failed`
warning with the adapter's fixed sanitized outcome. Failed body copies emit only
`media stream failed`; canceled public requests remain quiet. No request IDs,
asset IDs, provider paths, Range/If-Range values or body text are logged.
The consumer handler emits `provider search failed` with fixed provider failure
outcomes, mapping a search-endpoint HTTP 404 to unexpected provider status. It keeps
successes, invalid pagination, private/missing denials and cancellation quiet.
Consumer cursors and search results are never logged.
The standard HTTP server's connection error logger is discarded because its free
text can include request-controlled data; lifecycle failures are reported separately
through `slog`. Streaming failures use `http.ErrAbortHandler` to terminate incomplete responses
without free-text panic logging. Preserve privacy and bounded diagnostic volume
when changing this behavior.

## Operational split

Conceptually:

```text
Internet request
    |
    v
nginx
    |-- access/error log: HTTP edge/transport facts
    |
    v
Media Gateway
    |-- slog -> stderr -> journald: lifecycle, provider, policy/runtime diagnostics
    |
    v
private provider
```

This keeps the service small: no logging framework, log database, agent, or application-managed rotation is required.

## Production checks

Before production qualification, verify that:

1. normal eligible delivery is visible in the intended nginx access log;
2. gateway lifecycle/provider failures are visible with `journalctl -u media-gateway`;
3. a known private request does not reveal private path/provider metadata in either log;
4. provider credentials do not appear in nginx or journald output;
5. repeated denied requests cannot create an obvious unbounded high-severity log flood.
