# Media Gateway

Media Gateway is a minimal publication boundary for serving explicitly eligible media from a private provider without exposing the provider itself to the Internet.

The initial provider is Immich. The first deployment is intended to publish selected images from a private home archive through `media.stefk.me` while Immich remains LAN/loopback-only.

## Documentation

- [Architecture](architecture.md) — components, trust boundaries, policy model and request flow.
- [Security](security.md) — threats, non-negotiable invariants and fail-closed behavior.
- [Toolchain](toolchain.md) — Go version, dependency policy and CI/build baseline.
- [Logging](logging.md) — nginx access logging versus application/journald diagnostics.
- [Deployment](deployment.md) — configuration, systemd, nginx and host integration.
- [Roadmap](roadmap.md) — V0 capability boundaries and later work.

## V0 at a glance

```text
private archive
      ↓
    Immich
      ↓ private API
Media Gateway
      ↓ loopback
    nginx
      ↓ HTTPS
media.stefk.me
```

The service is intentionally not a media-management UI or a generic proxy. The configured publication policy is re-evaluated for every delivered asset.

V0 uses the Go 1.27 family (initial reviewed toolchain Go 1.27.1), the Go standard library for HTTP/logging/testing, and one expected external runtime dependency for strict TOML decoding. See [Toolchain](toolchain.md) for the exact policy.

## Source

Repository: [stef-k/media-gateway](https://github.com/stef-k/media-gateway)
