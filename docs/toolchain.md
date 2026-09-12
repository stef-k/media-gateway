# Toolchain

Media Gateway intentionally keeps its implementation stack small and auditable.

## Go baseline

V0 targets **Go 1.27.1**, the current stable Go release selected on 2026-09-12.

Go does not publish a separate LTS channel. Its release policy supports each major release until two newer major releases exist, with critical/security fixes shipped as minor revisions. For this project, "latest stable" therefore means the newest supported stable Go release, updated deliberately rather than automatically.

Project policy:

- language/toolchain family: Go 1.27;
- initial development and CI toolchain: Go 1.27.1;
- use stable releases only; no beta/RC toolchains;
- update patch releases promptly after normal CI/security review;
- major Go upgrades are deliberate changes with full tests and deployment validation;
- Linux amd64 is the first production target, but application code should remain portable where that costs nothing.

When issue #2 creates `go.mod`, prefer a minimum language line compatible with the selected family and an explicit toolchain directive for the reviewed patch release rather than silently depending on whatever Go happens to be installed on a developer machine.

Official release policy/history: <https://go.dev/doc/devel/release>

## Dependency policy

Use the Go standard library unless a small external dependency clearly reduces risk or complexity.

V0 should not add a web framework, router framework, DI container, logging framework, ORM, database driver, background-job framework or configuration framework.

The Go standard library already provides the expected V0 needs:

- HTTP server/client and routing: `net/http`;
- structured logging: `log/slog`;
- CLI flags: `flag`;
- contexts/timeouts/cancellation: `context` and `time`;
- URL/path handling: `net/url`, `path`, `strings` and related packages;
- cryptographic primitives if later required: `crypto/*`;
- testing/fuzzing: `testing`.

### TOML

The one expected third-party runtime dependency for #2 is:

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

No other third-party runtime dependency is approved for V0 by default. An implementation issue may add one only with a concrete requirement and documentation of why the standard library is insufficient.

## Configuration contract

TOML values are deployment configuration, not hard-coded product assumptions.

Operators may configure, subject to strict validation:

- gateway listen address;
- public base URL/hostname when needed;
- provider type and trusted provider base URL;
- provider credential file path;
- provider request timeout;
- one or more provider-visible allowed roots;
- literal publication directory-segment names and permitted media types;
- supported delivery options.

Security semantics are not configurable:

- deny by default;
- allowed roots use normalized path-aware containment, never string-prefix matching;
- publication names match one exact directory segment, never glob/regex/prefix patterns;
- request callers cannot supply or override upstream URLs or provider paths;
- unknown TOML fields are rejected rather than ignored;
- malformed, duplicate or ambiguous policy must prevent startup;
- unsupported media/representations fail closed.

## Build and CI baseline

Issue #2 should establish a single lightweight CI job on `ubuntu-latest` using the pinned Go family and run at minimum:

```text
gofmt check
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/media-gateway
```

Fuzz/property tests for the path-policy boundary belong in #3. Cross-compilation/release packaging belongs later when the service implementation is stable enough to justify it.

## Version-update policy

Dependency and toolchain upgrades are deliberate pull requests. Each upgrade should state what changed, run the normal tests, and avoid bundling unrelated refactors.

For Go patch updates, normal build/test/security validation is sufficient unless release notes identify a relevant runtime/network behavior change. For Go major releases or TOML major-version changes, review compatibility and configuration parsing behavior explicitly.
