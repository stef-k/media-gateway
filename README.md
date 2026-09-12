# Media Gateway

Media Gateway is a small, fail-closed publication gateway for serving explicitly eligible media from a private media provider without exposing the provider itself to the Internet.

The initial provider is [Immich](https://immich.app/). The motivating deployment keeps Immich and the photo archive private on a home LAN while publishing selected media through `https://media.stefk.me` for consumers such as WordPress.

> **Status:** architecture/bootstrap phase. There is no production release yet.

## Core idea

```text
private archive -> Immich -> Media Gateway -> nginx/HTTPS -> Internet
                         ^
                         |
                  private API only
```

Publication is **deny by default**. For the initial deployment, filesystem organization indexed by Immich is the single publication-policy source of truth:

- exact `public` directory segment: image/video eligible;
- exact `public-images` segment: image eligible;
- exact `public-videos` segment: video eligible;
- anything else: private.

Eligibility is checked by Media Gateway on every delivery request. Immich remains private; the gateway never exposes arbitrary provider URLs, NAS paths, or a generic proxy surface.

## V0 direction

V0 is intentionally small:

- one Go binary;
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

Image delivery is the first production target. Video delivery may follow once range/streaming behavior is specified and tested.

## Documentation

- [Architecture](docs/architecture.md)
- [Security model](docs/security.md)
- [Deployment](docs/deployment.md)
- [Roadmap](docs/roadmap.md)
- [GitHub Pages documentation](docs/index.md)

Deployment templates live under [`deploy/`](deploy/). They are examples and must be reviewed for the target host before installation.

## Project boundaries

Media Gateway is a publication boundary, not a media manager. It does not replace Immich, organize a photo library, provide a gallery UI, or decide which WordPress post should use an image.

Consumers may reference eligible assets, but consumer state does not grant publication permission. A consumer compromise must not turn a private Immich asset into public media.

## License

MIT. See [LICENSE](LICENSE).
