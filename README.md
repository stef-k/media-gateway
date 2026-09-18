# Media Gateway

Media Gateway is a small, fail-closed publication gateway for serving explicitly eligible media from a private media provider without exposing the provider itself to the Internet.

The initial provider is [Immich](https://immich.app/). The motivating deployment keeps Immich and the photo archive private on a home LAN while publishing selected media through a dedicated gateway hostname for consumers such as WordPress or Wayfarer.

> **Status:** the V0 security/deployment foundation is accepted at `d3948b7e2e9de9135fbd353a10b371fb727e9be3`: real Immich image-preview privacy/lifecycle behavior, trusted loopback consumer access, hardened systemd/nginx deployment, bundle/smoke and rollback are qualified. [#37](https://github.com/stef-k/media-gateway/issues/37) tracks product completion against the original requirement: named/scoped publication roots, paginated image+video browsing, original-resolution media delivery and range-capable video. The real public hostname/edge cutover remains separate and should follow #37.

## Core idea

```text
private archive -> Immich -> Media Gateway -> nginx/HTTPS -> Internet
                         ^
                         |
                  private API only
```

Publication is **deny by default**. Filesystem organization indexed by Immich is the publication-policy source of truth. Operators configure exact literal directory conventions such as `public`, `public-images`, `public-videos` or a root-scoped legacy convention such as `post`.

Anything outside a matching publication convention remains private. Moving an asset out of a matching directory revokes future delivery because every public request re-fetches current provider metadata/lifecycle and re-evaluates policy.

Immich remains private; the gateway never exposes arbitrary provider URLs, NAS paths, or a generic proxy surface.

## Accepted V0 foundation

V0 intentionally proved one narrow image-preview path before expanding the media plane:

- one Go binary;
- Go 1.27.1 reviewed toolchain baseline;
- one expected third-party runtime dependency: `github.com/pelletier/go-toml/v2` for strict TOML decoding;
- strict TOML configuration plus separately protected Immich credential;
- loopback-only application listener;
- fixed private Immich authority with no ambient proxy forwarding its credential;
- exact path/media publication policy;
- current asset lifecycle revalidation;
- public `GET`/`HEAD /media/<asset-id>/preview` image delivery;
- same-host trusted `/internal/assets` image browse/detail;
- validated optional coordinate projection in the V0 contract;
- nginx-only public boundary;
- unprivileged systemd service with zero capabilities;
- Linux amd64 bundle, checksums, portable smoke, upgrade and rollback procedure;
- no database, UI, direct NAS mount, generic URL fetching or public Immich endpoint.

The accepted image preview representation is a direct JPEG/WebP provider derivative with known positive length up to 16 MiB, no redirect/original fallback and `Cache-Control: no-store`. Private/missing/invalid assets share fixed denial; provider failures are sanitized. Representative real Immich 3.2.0 phone/camera/RAW previews and same-ID lifecycle revocation were qualified.

That V0 work is retained as the security/deployment foundation; it is not the final product definition.

## Product completion (#37)

The original product requirement is broader: a secure convention-driven **media** gateway that lets trusted consumer applications browse/select currently public media and serve the actual authorized media through stable gateway URLs.

See [Product completion](docs/product-completion.md) for the authoritative target design.

The deterministic lane is:

```text
#38 named roots + global/root-scoped policy rules
 -> #39 paginated eligible image/video collections and assets
 -> #40 original image delivery
 -> #41 video preview/original + byte ranges + long-media streaming
 -> #42 final config/docs/bundle/real-host qualification
 -> close #37
```

### Current publication configuration (Policy v2)

Policy v2 uses named provider roots and rules that may be global or caged to selected roots:

```toml
[[policy.roots]]
name = "images"
path = "/media/archive/Images"

[[policy.roots]]
name = "art"
path = "/media/archive/ART"

[[policy.rules]]
segment = "public"
media = ["image", "video"]

[[policy.rules]]
segment = "public-images"
media = ["image"]

[[policy.rules]]
segment = "public-videos"
media = ["video"]

[[policy.rules]]
segment = "post"
media = ["image"]
roots = ["images"]
```

Root paths are provider-visible absolute POSIX metadata paths, not Windows UNC/NAS paths and not local filesystem mounts. Root names are stable logical identifiers. Omitted rule `roots` means global; an explicit list scopes that convention to the named roots.

### Current trusted catalogue

Trusted same-host consumers page through publication collections and then page through assets inside a selected collection. Large directories must never be returned unbounded.

Routes:

```text
GET /internal/collections?limit=<n>&cursor=<opaque>
GET /internal/assets?root=<logical-root>&collection=<relative-path>&limit=<n>&cursor=<opaque>
GET /internal/assets/<asset-id>
```

Safe catalogue items include logical root, root-relative collection path, filename, image/video type, nullable dimensions and `duration_ms` (milliseconds), validated capture/local time and always-present nullable coordinates. Images and videos have preview and original gateway paths. These advertise implemented capabilities without probing representation existence. Collection pagination is at least once with in-page deduplication; consumers merge by `(root, collection_path)`. Gateway-signed cursors bind the query/policy and expire on process restart. Absolute provider/NAS paths, provider URLs, credentials and raw EXIF remain private.

Consumer selection never makes an asset public. Every delivery request independently reauthorizes current provider state.

### Current image and video routes

The product requires two fixed representations:

```text
GET/HEAD /media/<asset-id>/preview
GET/HEAD /media/<asset-id>/original
```

`preview` is a provider-generated browse/picker/poster representation. `original` means the provider original bytes after current authorization. The gateway does not silently convert RAW/HEIC/video originals or strip metadata from an original file; downstream consumers own resizing/derivatives as needed.

Video originals support one validated byte range with 200/206/416 and HEAD framing. Immich 3.2.0's range 404 is disambiguated with one fixed no-Range original GET: validated length proves public 416, while missing originals remain 404 and contradictions fail with 502. An oversized/equal suffix reuses the validated no-Range original body as full-representation 206; HEAD returns identical framing and closes it without reading. Other probe bodies are closed without streaming. Image originals and all previews ignore Range. Conditional requests remain unsupported. Established image/video originals use 60-second read/write inactivity bounds; authorization and provider connection/header work stay bounded. Exact M6 Immich 3.2.0 range and long-stream qualification remains required before #41 merge.

## Running the current service

Build with Go 1.27.1 and run with an explicit configuration path:

```sh
go build -o bin/media-gateway ./cmd/media-gateway
bin/media-gateway -version
bin/media-gateway -config /etc/media-gateway/config.toml
```

The current service uses Policy v2, paginated image/video collections and assets, fixed previews, and policy-checked image/video originals. #41 software implements video ranges and long streaming; M6 qualification is pending. #42 retains final product reconciliation. See [Configuration](docs/configuration.md) and [Private consumer API](docs/consumer-api.md) for current behavior; see [Product completion](docs/product-completion.md) for the target contract. Image originals preserve exact source bytes, including EXIF/GPS. Their provider GET uses no query and requires `asset.download` in addition to `asset.read` and `asset.view`. Original streams refresh finite per-I/O deadlines; ordinary responses retain their finite handler/server bounds. Video posters use only Immich thumbnail size=preview, and originals never use playback/transcoded bytes. Remove obsolete `[delivery]` configuration; it now fails startup with migration guidance. There is no `[consumer]` config section; stale `consumer.expose_coordinates` fails with migration guidance. Catalogue search uses structured Immich metadata search, never folder view.

See [Release and smoke procedure](docs/release.md) for bundles and rollback, [Deployment](docs/deployment.md) for operational bounds, and [Toolchain](docs/toolchain.md) for local/CI validation.

## Logging

Logging remains deliberately split rather than duplicated:

- nginx owns public HTTP access and proxy/transport logging;
- Media Gateway uses Go `log/slog` for lifecycle, sanitized startup state and provider/runtime/security diagnostics;
- under systemd, application logs go to stderr/stdout and journald owns storage/rotation;
- the Go service does not implement its own logfile/rotation subsystem or a second full access log.

See [Logging](docs/logging.md) for privacy and severity rules.

## Documentation

- [Product completion design](docs/product-completion.md)
- [Architecture](docs/architecture.md)
- [Security model](docs/security.md)
- [Private consumer API](docs/consumer-api.md)
- [Configuration loading and validation](docs/configuration.md)
- [Toolchain and dependency policy](docs/toolchain.md)
- [Logging](docs/logging.md)
- [Deployment](docs/deployment.md)
- [Roadmap](docs/roadmap.md)
- [GitHub Pages documentation](docs/index.md)

Deployment templates live under [`deploy/`](deploy/). They describe the current accepted implementation and must be reviewed for the target host. The shipped example uses Policy v2; stale `policy.allowed_roots` configuration fails startup with a migration diagnostic.

## Project boundaries

Media Gateway is a publication boundary, not a media manager. It does not replace Immich, organize the private photo/video archive, provide a gallery UI, or decide which WordPress/Wayfarer object should use an asset.

Consumers may browse and reference eligible assets, but consumer state never grants publication permission. A consumer compromise must not turn a private Immich asset into public media.

## License

MIT. See [LICENSE](LICENSE).
