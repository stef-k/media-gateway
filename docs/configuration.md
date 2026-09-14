# Configuration foundation

`internal/config.Load(filename)` reads an explicitly supplied TOML file and returns
validated typed configuration, a separate credential string, and an error. It does
not start a listener, contact Immich, evaluate asset authorization or deliver media.
Any failure returns zero configuration and an empty credential. No environment,
working-directory or home-directory configuration discovery occurs.

Use the commented [operator example](../deploy/config.toml.example). All fields are
required except `server.public_base_url`, `delivery.allow_original`, and
`consumer.expose_coordinates` (both booleans default to false; `[consumer]` may be
omitted). Unknown fields/tables, incorrect types and malformed TOML are rejected.
Configuration input is limited to 1 MiB. Parser errors are deliberately replaced with
a schema/syntax diagnostic because raw parser messages can quote private input.
Validation errors identify the offending field or operation without its value.

## Connectivity and credential

- `server.listen`: numeric loopback IP, including IPv6, and port 1..65535. No DNS
  resolution, wildcard binds, zone identifiers or ephemeral ports.
- `server.public_base_url`: optional HTTP(S) origin.
- `provider.type`: exactly `immich`.
- `provider.base_url`: required HTTP(S) origin. Both URL fields reject userinfo,
  query strings, fragments, non-root paths and invalid ports. A trailing `/` is
  accepted. The operator owns selection of the trusted private provider address.
- `provider.request_timeout`: required positive Go duration such as `15s`; invalid,
  zero, negative and overflowing values fail. It is returned as `time.Duration`.
- `provider.api_key_file`: absolute normalized host file path. The target must be a
  readable regular file with owner-read permission and no group/other permissions.
  Symlinks to protected regular files are allowed. Files are limited to 4096 bytes;
  their contents must be one non-empty printable ASCII token, optionally followed
  by LF or CRLF. File errors never include the path or credential contents.

The returned credential is intentionally separate from `Config`. Callers must never
log the credential or whole configuration: configuration includes private topology.
The loader itself emits no logs. Operators must keep configuration and credential
locations under trusted host control; this is not a hostile-filesystem sandbox.

## Installed ownership and startup state

Follow the [Linux installation procedure](deployment.md#service-account-and-installation):
root owns `/etc/media-gateway` (`0750`, group `media-gateway`) and `config.toml`
(`0640`, same group). The service can read TOML but cannot edit or replace it.
The key is owned by `media-gateway:media-gateway`, mode `0400`; root can administer
it. A root-owned group-readable `0640` key fails the loader's permission check.
Use regular files without extra ACL grants. The root-owned directory prevents
service-side replacement; the unit's read-only filesystem also blocks key changes.
The loader checks key mode, not all host ownership/ACL boundaries: operators must
maintain these permissions.

TOML and the key are loaded once before binding. **Changes require service restart**;
there is no hot reload, reload signal or `ExecReload`. `daemon-reload` only rereads
systemd units and does not apply TOML/key changes. Invalid configuration, unreadable
or invalid keys, and listener setup failures exit nonzero without serving requests.
There is no separate validation-only CLI: startup performs validation. A changed
file does not affect the still-running process until restart.

## Policy and delivery

`policy.allowed_roots` requires at least one unique, absolute normalized POSIX path.
It rejects relative/empty roots, dot components, repeated/trailing slashes (except
`/`), backslashes and control characters. Values are validated, never silently
cleaned. Nested roots are permitted because all roots share the same global rules;
they do not create conflicting per-root permissions.

`policy.rules` requires at least one rule. Each segment must be unique, non-empty,
and a single literal directory component other than `.` or `..`. Separators,
control characters, surrounding whitespace and pattern punctuation
(`* ? [ ] { } ( ) | ^ $ +`) are rejected. Each media list must contain one or both of
`image` and `video`, without duplicates. Different segments may classify the same
media type. Installation-specific names such as `/external/photos` and `website`
are supported. This validation does not implement path matching or authorize assets.

`delivery.allow_original` must be false. `delivery.image_variant` must be `preview`.
Accepting a video policy rule does not enable video delivery. Accepting `preview`
alone does not establish privacy or quality: accepted #18 qualified representative
Immich 3.2.0 previews; provider/settings changes require renewed evidence. The route accepts
only JPEG/WebP previews with a positive known length of at most 16 MiB. The
provider key needs `asset.read` and `asset.view`; per-call timeouts include
streaming and are also capped by the 60-second public request context.

## Trusted consumer coordinates

```toml
[consumer]
# Opt-in for eligible loopback consumer metadata only; no change to preview bytes.
expose_coordinates = false
```

`consumer.expose_coordinates` must be a boolean. An omitted section or flag is
false: search retains `withExif=false` and the six-field consumer JSON shape.
True changes only fixed consumer search to `withExif=true` and projects validated
latitude/longitude after current publication eligibility. Both keys are then
numbers or both `null`; a partial, nonnumeric, non-finite or out-of-range pair
fails through the sanitized provider-failure path. See [consumer API](consumer-api.md).

This is deliberate coordinate disclosure to trusted same-host consumers, which
may publish those coordinates through their own UX. It does not expose raw EXIF,
add public metadata routes or change `/media/<id>/preview` bytes. nginx must never
publish `/internal/`. Changes require restart; startup logs contain no coordinates
or provider metadata. There are no per-asset overrides, geocoding or caches.

## Development validation

With the reviewed Go 1.27.1 toolchain, run:

```sh
test -z "$(gofmt -l internal/config)"
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

The module pins `github.com/pelletier/go-toml/v2` v2.4.3 as its only runtime
dependency. The executable, lifecycle and CI workflow belong to issue #10.
