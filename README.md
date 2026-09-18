# Media Gateway

A small, fail-closed publication gateway for images and videos managed by private
[Immich](https://immich.app/). Publish through exact directory conventions beneath
named provider roots; trusted same-host applications browse eligible collections,
and public clients receive stable preview and original URLs.

**[Read the documentation](https://stef-k.github.io/media-gateway/)** ·
[Documentation source](docs/index.md) · [Configuration example](deploy/config.toml.example)

Every delivery rechecks current provider lifecycle and publication policy.
Consumer selections never grant publication. Immich and NAS paths stay private;
the gateway does not mount storage, transform media or provide a gallery UI.
Originals preserve source bytes, including embedded EXIF/GPS.

- Policy v2 named roots with global and root-scoped exact conventions.
- Paginated image/video collections and safe metadata on loopback `/internal/`.
- Public `GET`/`HEAD /media/<id>/preview` and `/media/<id>/original`.
- Video byte ranges, validated 206/416 framing and inactivity-bounded streaming.
- One Go binary, strict TOML, separate credential, systemd/nginx templates and
  a checksummed Linux bundle with portable smoke and rollback guidance.

## Status and getting started

Feature children #38–#41 are accepted on `main` through
`7053ac1f297b97be6daacca7ff450b442cb689e7` (PR #47).
[#42](https://github.com/stef-k/media-gateway/issues/42) owns final documentation,
bundle and disposable qualification; [#37](https://github.com/stef-k/media-gateway/issues/37)
remains open until final acceptance. Production cutover belongs to
[server-migration #10](https://github.com/stef-k/server-migration/issues/10).

Start with the [operator quick start](docs/configuration.md#get-started),
[consumer guide](docs/consumer-api.md), or [release and operations](docs/release.md).
Build and validate using the reviewed [Go 1.27.1 toolchain](docs/toolchain.md).
Additional derivative profiles in #30 remain optional.

## License

[MIT](LICENSE).
