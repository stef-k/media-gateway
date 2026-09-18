# Media Gateway

A small, fail-closed publication gateway for images and videos managed by private
[Immich](https://immich.app/). Publish through exact directory conventions beneath
named provider roots; trusted same-host applications browse eligible collections,
and public clients receive stable preview and original URLs.

**[Read the documentation](https://stef-k.github.io/media-gateway/)** ·
[GitHub Releases](https://github.com/stef-k/media-gateway/releases) ·
[Changelog](CHANGELOG.md) · [Documentation source](docs/index.md) · [Configuration example](deploy/config.toml.example)

Every delivery rechecks current provider lifecycle and publication policy.
Consumer selections never grant publication. Immich and NAS paths stay private;
the gateway does not mount storage, transform media or provide a gallery UI.
Originals preserve source bytes, including embedded EXIF/GPS.

- Named roots with global and root-scoped exact conventions.
- Paginated image/video collections and safe metadata on loopback `/internal/`.
- Public `GET`/`HEAD /media/<id>/preview` and `/media/<id>/original`.
- Video byte ranges, validated 206/416 framing and inactivity-bounded streaming.
- One Go binary, strict TOML, separate credential, systemd/nginx templates and
  a checksummed Linux bundle with portable smoke and rollback guidance.

Source-metadata exposure is **off by default**. Capture/local timestamps,
coordinates and original paths remain present as null; originals return fixed 404
without provider fetches. Qualified previews/posters remain available. Set
`privacy.expose_source_metadata=true` deliberately to enable sensitive catalogue
fields and exact originals, whose bytes may contain embedded metadata. Filenames
and collection names remain visible in both modes.

## Getting started

The supported provider is Immich; deployed contract validation used Immich 3.2.0.
The reference platform is Linux amd64 with systemd and nginx.

Start with the [operator quick start](docs/configuration.md#get-started),
[consumer guide](docs/consumer-api.md), or [release and operations](docs/release.md).
Download supported binaries from [GitHub Releases](https://github.com/stef-k/media-gateway/releases)
and follow [archive verification](docs/release.md#download-and-verify-a-release).
Source builds use the reviewed [Go 1.27.1 toolchain](docs/toolchain.md).

## License

[MIT](LICENSE).
