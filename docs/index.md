# Media Gateway

Media Gateway is a minimal fail-closed publication boundary for serving explicitly eligible media from a private provider without exposing the provider itself to the Internet.

The initial provider is Immich. V0 proved the image-preview security/deployment boundary; [#37](https://github.com/stef-k/media-gateway/issues/37) now completes the original convention-driven image/video product requirement before the real public hostname/edge cutover.

## Documentation

- [Product completion](product-completion.md) — authoritative target contract for named/scoped publication roots, paginated image/video browsing, originals and video ranges.
- [Architecture](architecture.md) — accepted components, trust boundaries, policy model and request flow.
- [Security](security.md) — threats, non-negotiable invariants and fail-closed behavior.
- [Private consumer API](consumer-api.md) — current paginated image/video collections, signed cursors and safe nullable metadata.
- [Configuration](configuration.md) — current Policy v2 schema and strict coordinate-setting migration.
- [Toolchain](toolchain.md) — Go version, dependency policy and CI/build baseline.
- [Logging](logging.md) — nginx access logging versus application/journald diagnostics.
- [Deployment](deployment.md) — systemd/nginx/release host integration baseline.
- [Roadmap](roadmap.md) — accepted V0 foundation and deterministic #37 product-completion lane.

## Architecture at a glance

```text
private archive
      ↓
    Immich
      ↓ private API
Media Gateway
      ↓ loopback
    nginx
      ↓ HTTPS
public consumer/browser
```

Publication authority comes from configured provider-root and exact directory conventions, not from consumer selections. Every delivered asset is re-evaluated against current provider lifecycle/policy.

The service is intentionally not a media-management UI, NAS browser or generic proxy.

## Current versus target

The current service publicly delivers image `/preview` and `/original` (M6 original qualification pending) and provides paginated trusted image/video collections and assets. Video/range/long-stream delivery remains in the #37 lane:

- policy-checked provider-original video delivery;
- range-capable video and long-media streaming semantics.

Do not treat target routes/schema as implemented until their owning issues are accepted.

## Source

Repository: [stef-k/media-gateway](https://github.com/stef-k/media-gateway)
