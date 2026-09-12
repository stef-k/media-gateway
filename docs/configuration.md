# Configuration foundation

`internal/config.Load(filename)` reads an explicitly supplied TOML file and returns
validated typed configuration, a separate credential string, and an error. It does
not start a listener, contact Immich, evaluate asset authorization or deliver media.
Any failure returns zero configuration and an empty credential. No environment,
working-directory or home-directory configuration discovery occurs.

Use the commented [operator example](../deploy/config.toml.example). All fields are
required except `server.public_base_url` and `delivery.allow_original` (which defaults
to false). Unknown fields/tables, incorrect types and malformed TOML are rejected.
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
does not establish its privacy or quality: real-provider qualification in #18
remains open. The implemented route accepts
only JPEG/WebP previews with a positive known length of at most 16 MiB. The
provider key needs `asset.read` and `asset.view`; per-call timeouts include
streaming and are also capped by the 60-second public request context.

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
