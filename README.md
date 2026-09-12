# Media Gateway

Media Gateway is a small, fail-closed publication gateway for serving explicitly eligible media from a private media provider without exposing the provider itself to the Internet.

The initial provider is [Immich](https://immich.app/). The motivating deployment keeps Immich and the photo archive private on a home LAN while publishing selected media through `https://media.stefk.me` for consumers such as WordPress.

> **Status:** fail-closed public image preview delivery implemented with fake-provider tests. Real-Immich privacy/quality qualification remains open in #18; there is no production release.

## Core idea

```text
private archive -> Immich -> Media Gateway -> nginx/HTTPS -> Internet
                         ^
                         |
                  private API only
```

Publication is **deny by default**. For the motivating deployment, filesystem organization indexed by Immich is the single publication-policy source of truth. The names below are examples configured through TOML, not hard-coded universal requirements:

- exact `public` directory segment: image/video eligible;
- exact `public-images` segment: image eligible;
- exact `public-videos` segment: video eligible;
- anything else: private.

Eligibility is checked by Media Gateway on every delivery request. Immich remains private; the gateway never exposes arbitrary provider URLs, NAS paths, or a generic proxy surface.

## V0 direction

V0 is intentionally small:

- one Go binary;
- **Go 1.27.1** initial stable toolchain baseline;
- one expected third-party runtime dependency: `github.com/pelletier/go-toml/v2` for strict TOML decoding;
- one TOML configuration file plus a separately protected Immich credential;
- loopback-only application listener;
- Immich metadata and preview retrieval;
- exact path/media-type publication policy;
- public read-only media delivery behind nginx;
- no database;
- no UI;
- no direct NAS mount;
- no generic URL fetching;
- no public Immich endpoint.

Go has no separate LTS channel; the project follows supported stable Go releases deliberately, with patch updates reviewed and major upgrades tested rather than taken automatically.

Image delivery is the first production target. Video delivery may follow once range/streaming behavior is specified and tested.

The foundation epic is intentionally decomposed before implementation: #9 owns the Go/configuration core and #10 owns the executable lifecycle, logging and CI shell. Coarse epics are not handed to coding agents when a bounded child issue exists.

## Running the service

Build with Go 1.27.1 and run with an explicit configuration path:

```sh
go build -o bin/media-gateway ./cmd/media-gateway
bin/media-gateway -version
bin/media-gateway -config /etc/media-gateway/config.toml
```

The configuration and separate credential must pass [`config.Load`](docs/configuration.md)
validation before binding. `GET` and `HEAD /media/<asset-id>/preview` fetch current
Immich metadata and authorize the image through `publication.Eligible` before
requesting a preview. The key needs `asset.read` and `asset.view`. All other
routes/methods are denied; there is no health/readiness endpoint. A running
listener does not establish provider or media readiness. SIGINT/SIGTERM stop accepting
connections and allow up to 10 seconds for active requests to drain.

Previews must be direct HTTP 200 JPEG/WebP responses with a known positive length
of at most 16 MiB. Delivery streams without whole-image buffering, rejects every
provider redirect, and uses `Cache-Control: no-store`. Private/missing/invalid
assets return fixed `404` denials; provider failures return fixed `502` responses.
A failure after streaming begins aborts the response. Originals, video, range
and conditional delivery are unavailable. Source review and synthetic tests do
not establish EXIF/GPS privacy or visual quality; #18 must qualify representative
real Immich previews before production use.

See [Deployment](docs/deployment.md) for exit codes and operational bounds, and
[Toolchain](docs/toolchain.md) for the local/CI validation commands.

## Logging

Logging is deliberately split rather than duplicated:

- nginx owns public HTTP access and proxy/transport logging;
- Media Gateway uses Go `log/slog` for lifecycle, sanitized startup state and provider/runtime/security diagnostics;
- under systemd, application logs go to stderr/stdout and journald owns storage/rotation;
- the Go service does not implement its own logfile/rotation subsystem or a second full access log.

See [Logging](docs/logging.md) for privacy and severity rules.

## Documentation

- [Architecture](docs/architecture.md)
- [Security model](docs/security.md)
- [Configuration loading and validation](docs/configuration.md)
- [Toolchain and dependency policy](docs/toolchain.md)
- [Logging](docs/logging.md)
- [Deployment](docs/deployment.md)
- [Roadmap](docs/roadmap.md)
- [GitHub Pages documentation](docs/index.md)

Deployment templates live under [`deploy/`](deploy/). They are examples and must be reviewed for the target host before installation. The TOML, nginx and systemd examples are intentionally comment-documented so operators can understand which values are deployment-specific and which constraints are security invariants.

## Project boundaries

Media Gateway is a publication boundary, not a media manager. It does not replace Immich, organize a photo library, provide a gallery UI, or decide which WordPress post should use an image.

Consumers may reference eligible assets, but consumer state does not grant publication permission. A consumer compromise must not turn a private Immich asset into public media.

## License

MIT. See [LICENSE](LICENSE).
