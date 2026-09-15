# Configuration foundation

> **Current vs target:** this file documents the accepted V0 configuration implemented on `main` at the start of #37. The product-complete replacement for publication roots/rules is defined in [product completion](product-completion.md) and issue #38. Do not use the target TOML against current V0 binaries until #38 is accepted.

`internal/config.Load(filename)` reads an explicitly supplied TOML file and returns validated typed configuration, a separate credential string, and an error. It does not start a listener, contact Immich, evaluate asset authorization or deliver media. Any failure returns zero configuration and an empty credential. No environment, working-directory or home-directory configuration discovery occurs.

Use the commented [operator example](../deploy/config.toml.example) for the **current accepted implementation**. Unknown fields/tables, incorrect types and malformed TOML are rejected. Configuration input is limited to 1 MiB. Parser errors are deliberately replaced with a schema/syntax diagnostic because raw parser messages can quote private input. Validation errors identify the offending field or operation without its value.

## Connectivity and credential

These requirements remain product invariants through #37:

- `server.listen`: numeric loopback IP, including IPv6, and port 1..65535. No DNS resolution, wildcard binds, zone identifiers or ephemeral ports.
- `server.public_base_url`: optional HTTP(S) origin.
- `provider.type`: exactly `immich` until another provider creates a real requirement.
- `provider.base_url`: required trusted HTTP(S) origin; reject userinfo, query, fragment, non-root path and invalid ports.
- `provider.request_timeout`: required positive Go duration for bounded provider setup/metadata work. #41 may refine streaming lifetime separately from this request/open bound.
- `provider.api_key_file`: absolute normalized host file path to the separately protected credential.

The credential is intentionally separate from `Config`. Never log the credential or the whole configuration because configuration contains private topology.

## Installed ownership and startup state

Follow [deployment](deployment.md#service-account-and-installation). The accepted reference contract uses a root-controlled `/etc/media-gateway`, TOML readable but not writable by the service, and a restrictive separate key readable only by the required service/admin identities.

TOML and the key are loaded once before binding. **Changes require service restart**. There is no hot reload or `ExecReload`; #37 does not change that requirement.

## Current V0 policy/delivery schema

Current `main` uses:

```toml
[policy]
allowed_roots = ["/media/archive"]

[[policy.rules]]
segment = "public"
media = ["image", "video"]
```

`policy.allowed_roots` requires unique absolute normalized POSIX provider paths. `policy.rules` are global across every allowed root and use exact literal directory-component matching. Media values are `image` and/or `video`.

Current V0 also requires:

```toml
[delivery]
allow_original = false
image_variant = "preview"

[consumer]
expose_coordinates = false
```

The accepted public handler therefore delivers image preview only, and consumer coordinates are currently opt-in. These are known product-slice limitations rather than the final #37 contract.

## Target policy v2 (#38)

The final product replaces flat `allowed_roots` with named provider roots and allows rules to be global or caged to selected roots.

Target shape:

```toml
[policy]

[[policy.roots]]
name = "images"
path = "/media/archive/Images"

[[policy.roots]]
name = "art"
path = "/media/archive/ART"

# Global convention: applies below every root.
[[policy.rules]]
segment = "public"
media = ["image", "video"]

[[policy.rules]]
segment = "public-images"
media = ["image"]

[[policy.rules]]
segment = "public-videos"
media = ["video"]

# Root-scoped legacy convention.
[[policy.rules]]
segment = "post"
media = ["image"]
roots = ["images"]
```

### Root paths

`path` is the absolute normalized POSIX path **reported by the provider in asset metadata**. It is not a Windows UNC path such as `\\NAS\Multimedia\Images`, not a host NAS mount and never a local path opened by Media Gateway.

`name` is stable logical configuration identity. Consumers may receive this logical name; they must never receive the root's absolute provider path.

#38 must reject duplicate/unsafe names, duplicate paths and overlapping roots so one provider asset cannot belong ambiguously to multiple configured roots.

### Rule scoping

Rules remain OR-ed exact conventions.

- omitted `roots` -> applies to every configured root;
- `roots = ["images"]` -> applies only beneath that named root;
- explicit `roots = []` -> invalid;
- unknown/duplicated root references -> invalid;
- segment matching remains exact component equality at any descendant depth;
- `post` never matches `post process`;
- media remains provider-normalized `image`/`video`.

The policy layer should return enough matched logical-root context for #39 catalogue projection without making absolute provider paths consumer data.

## Target delivery/consumer configuration cleanup

The final #37 product requires fixed gateway `preview` and `original` representations. Publication policy decides whether media may be public; the current `[delivery] allow_original=false/image_variant=preview` gate is therefore expected to be removed or replaced by a smaller actual requirement during #40/#42 rather than carried as a permanent contradictory switch.

Likewise, validated latitude/longitude are part of the normal trusted catalogue requirement in #39. The current `[consumer].expose_coordinates` feature flag is expected to disappear from the final schema. Continue to expose only the validated nullable coordinate pair; raw EXIF remains private.

Do not remove either current field before its owning implementation issue migrates code/tests/docs strictly and makes stale configs fail clearly.

## Provider permissions

Current V0 preview documentation uses the permissions verified for metadata/preview. #40/#41 must immediately re-verify the deployed/supported Immich API and update the minimum dedicated-key permission set for original download/video/range behavior. Do not infer future permissions from old examples.

## Development validation

With the reviewed toolchain, run the normal repository baseline:

```sh
test -z "$(gofmt -l $(git ls-files '*.go'))"
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/media-gateway
git diff --check
```

Strict TOML decoding and fail-closed startup remain mandatory throughout #37.