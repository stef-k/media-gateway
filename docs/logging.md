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

The nginx access log is the primary request/access record. Media Gateway should not duplicate a second full access log for every successful request.

Public-edge logging must not record secrets. In particular:

- never log `Authorization` or provider credentials;
- avoid logging query strings if a future feature places signatures/tokens in them;
- do not expose provider/NAS paths through public URLs in the first place;
- review any custom nginx log format before enabling it on an Internet-facing deployment.

The V0 public route is intentionally path-based and requires no secret query parameter.

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

This keeps the service small: no logging framework, log database, agent, or application-managed rotation is required for V0.

## Production checks

Before production qualification, verify that:

1. normal eligible delivery is visible in the intended nginx access log;
2. gateway lifecycle/provider failures are visible with `journalctl -u media-gateway`;
3. a known private request does not reveal private path/provider metadata in either log;
4. provider credentials do not appear in nginx or journald output;
5. repeated denied requests cannot create an obvious unbounded high-severity log flood.
