---
title: "Toolchain"
---

# Toolchain

Media Gateway intentionally keeps its implementation stack small and auditable.

## Go baseline

The reviewed toolchain is **Go 1.27.1**.

Go does not publish a separate LTS channel. Its release policy supports each major release until two newer major releases exist, with critical/security fixes shipped as minor revisions. For this project, "latest stable" therefore means the newest supported stable Go release, updated deliberately rather than automatically.

Project policy:

- language/toolchain family: Go 1.27;
- development and CI toolchain: Go 1.27.1;
- use stable releases only; no beta/RC toolchains;
- update patch releases promptly after normal CI/security review;
- major Go upgrades are deliberate changes with full tests and deployment validation;
- Linux amd64 is the first production target, but application code should remain portable where that costs nothing.

`go.mod` declares Go 1.27.0 as the language minimum and Go 1.27.1 as the reviewed toolchain. CI explicitly selects Go 1.27.1.

Official release policy/history: <https://go.dev/doc/devel/release>

## Dependency policy

Use the Go standard library unless a small external dependency clearly reduces risk or complexity.

Do not add a web framework, router framework, DI container, logging framework, ORM, database driver, background-job framework or configuration framework.

The Go standard library already provides the product needs:

- HTTP server/client and routing: `net/http`;
- structured logging: `log/slog`;
- CLI flags: `flag`;
- contexts/timeouts/cancellation: `context` and `time`;
- URL/path handling: `net/url`, `path`, `strings` and related packages;
- cryptographic primitives if later required: `crypto/*`;
- testing/fuzzing: `testing`.

### TOML

The sole third-party runtime dependency is:

```text
github.com/pelletier/go-toml/v2 v2.4.3
```

Reasons:

- current stable v2 release at the time of selection;
- narrow responsibility: TOML parsing/encoding rather than a configuration framework;
- supports TOML 1.1;
- provides strict decoding through `Decoder.DisallowUnknownFields()`, which fits the fail-closed configuration requirement;
- pure Go and easy to audit/replace if needed.

Package documentation: <https://pkg.go.dev/github.com/pelletier/go-toml/v2>

No other third-party runtime dependency is approved by default. Add one only with a concrete requirement and documentation of why the standard library is insufficient.

## Configuration contract

TOML values are deployment configuration, not hard-coded product assumptions.

Operators may configure, subject to strict validation:

- gateway listen address;
- provider type and trusted provider base URL;
- provider credential file path;
- provider request timeout;
- one or more provider-visible allowed roots;
- literal publication directory-segment names and permitted media types.

Image/video preview/original are fixed product routes. The optional
`privacy.expose_source_metadata` flag defaults to false and controls approved
source-sensitive public catalog fields and exact original availability.

Security semantics are not configurable:

- deny by default;
- allowed roots use normalized path-aware containment, never string-prefix matching;
- publication names match one exact directory segment, never glob/regex/prefix patterns;
- request callers cannot supply or override upstream URLs or provider paths;
- unknown TOML fields are rejected rather than ignored;
- malformed, duplicate or ambiguous policy must prevent startup;
- unsupported media/representations fail closed.

## Build and CI baseline

The single Linux job in `.github/workflows/ci.yml` runs on pull requests and pushes to `main`. Run the same baseline locally with Go 1.27.1:

```sh
test -z "$(gofmt -l $(git ls-files '*.go'))"
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/media-gateway
python3 scripts/test_smoke.py
python3 scripts/test_nginx.py # Requires local nginx; CI installs it explicitly.
bash -n scripts/build-bundle.sh
```

`-version` reports module version, embedded VCS revision/dirty state and Go version.
Local builds may show a generated module pseudo-version or `(devel)`; absent VCS
revision/dirty metadata is reported as `unknown`.
At an exact release tag, Go 1.27.1 reads the semantic module version directly
from Git. Release validation requires the tag version, exact commit, clean state
and reviewed toolchain; no custom version injection or version file is used. `scripts/build-bundle.sh` builds Linux amd64
with CGO disabled from a clean checkout and packages an explicit file allowlist.
See [release validation](release.md); the procedure is repeatable, without claiming
byte-for-byte reproducibility. Python 3 and Linux host tools are operator smoke
tooling only, not application runtime dependencies.

The separate `.github/workflows/release.yml` validates explicit stable tags and
publishes only accepted main commits. Its PR path runs
`python3 scripts/test_release.py` with Python 3.12+: normal revision packaging plus full release
construction in a disposable locally tagged clone, without publication.

## Version-update policy

Dependency and toolchain upgrades are deliberate pull requests. Each upgrade should state what changed, run the normal tests, and avoid bundling unrelated refactors.

For Go patch updates, normal build/test/security validation is sufficient unless release notes identify a relevant runtime/network behavior change. For Go major releases or TOML major-version changes, review compatibility and configuration parsing behavior explicitly.
