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

Start from the repository's [`deploy/config.toml.example`](https://github.com/stef-k/media-gateway/blob/main/deploy/config.toml.example) once the implementation schema is finalized.

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

The final implementation should provide a `-config` or equivalent explicit command-line option matching the service file.

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

The provider API key should use the minimum current Immich permissions required for:

- asset metadata lookup;
- the selected preview/representation retrieval;
- eligible-asset search only if/when a private consumer API is implemented.

Re-check current Immich permissions/API behavior when implementing or upgrading provider support.

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
