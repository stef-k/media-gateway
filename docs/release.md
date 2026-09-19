---
title: "Linux bundle and deployment smoke"
---

# Linux bundle and deployment smoke

The Linux amd64 bundle includes the binary, deployment templates, offline
documentation and portable smoke tooling. Verify it before installation and
retain a coherent binary/configuration/template set for rollback.

CI retains the verified archive as the `media-gateway-linux-amd64` artifact.
CI artifacts are exact-revision qualification inputs, not the supported binary
download surface. Use [GitHub Releases](https://github.com/stef-k/media-gateway/releases)
for versioned distribution.

## Download and verify a release

Choose a specific immutable `vMAJOR.MINOR.PATCH` from
[GitHub Releases](https://github.com/stef-k/media-gateway/releases), and read its
notes and [CHANGELOG](https://github.com/stef-k/media-gateway/blob/main/CHANGELOG.md).
Linux amd64 is the supported binary platform. Download both assets into a new
empty directory. Replace `vX.Y.Z` below with the exact chosen release tag:

```sh
# Stop on any failure. Downloading/verifying does not install or restart services.
set -eu
version='vX.Y.Z' # Replace this placeholder with the selected immutable tag.
archive="media-gateway-$version-linux-amd64.tar.gz"
base="https://github.com/stef-k/media-gateway/releases/download/$version"
curl --fail --location --output "$archive" "$base/$archive"
curl --fail --location --output "$archive.sha256" "$base/$archive.sha256"
sha256sum --check --strict "$archive.sha256"
tar -xzf "$archive"
cd "media-gateway-$version-linux-amd64"
sha256sum --check --strict SHA256SUMS
./media-gateway -version
```

The external `.tar.gz.sha256` verifies the downloaded archive; internal
`SHA256SUMS` verifies its packaged files. Require the reported `version` to equal your selected `$version`, the revision to
match that exact tag commit shown on GitHub, `modified=false`, and the Go toolchain
recorded for that release (see its bundled [toolchain guide](toolchain.md)).
Checksums detect corruption; they are not independent publisher signatures.
Obtain both files from the intended repository's authenticated HTTPS release.

Downloading a release is separate from production deployment. After verification,
follow [first installation](#first-installation) or [upgrade and rollback](#upgrade-and-rollback)
with the existing [deployment guidance](deployment.md). No Go toolchain or CI access
is needed on the target. Retain the previous versioned release directory and its
coherent protected configuration/template backup until upgrade acceptance.

## Version and publication policy

The immutable Git tag is the sole version authority: `vMAJOR.MINOR.PATCH`, with
no leading zeroes, prerelease suffix or build suffix. Choose major versions for
incompatible operator/consumer contracts, minor versions for compatible additions,
and patch versions for compatible fixes. Never move/reuse a published tag or replace
its assets. Keep `Unreleased` above dated `## X.Y.Z - YYYY-MM-DD` sections in
`CHANGELOG.md`; each release needs one nonempty matching section before tagging.
Entries describe visible changes and compatibility, not commit history.

Before creating any real release tag, a repository administrator must enable
**release immutability** in the repository Settings. This is a maintainer release
prerequisite, not an operator installation requirement. The workflow cannot grant
or configure repository administration policy. With immutability enabled,
publication locks the release assets and associated tag and GitHub generates a
signed release attestation. Do not create a release tag before enabling this setting.

Maintainers first review and merge the release mechanism and changelog to `main`,
then separately create/push the chosen tag at that accepted commit. The release
workflow fetches main, checks ancestry/exact-tag clean source and changelog, runs
formatting/vet/tests/race/build/smoke regressions, and builds the distribution once
through `scripts/build-bundle.sh OUTPUT --release vX.Y.Z`. It verifies all internal
checksums, versioned layout, tagged-source assets and exact binary metadata before
creating the external checksum. Notes come from the matching changelog section.
Go 1.27.1 supplies the exact v1 tag version directly; there is no injected version.
Future major releases require reviewing Go module-path/tag compatibility: the
current module path does not embed v2+ tags as exact versions, and validation
rejects any tag/binary version mismatch.

Only an explicit new tag push can publish. PR release checks use a disposable
synthetic-tag clone and read-only permissions; main CI retains revision bundles.
Only the guarded publication job has `contents: write`. It consumes the validated
archive/checksum, creates a draft with both assets, then publishes it using `gh`.
Immediately afterward it queries the published release and requires GitHub to
report `immutable: true`; any other result or query failure fails the workflow.
This assertion detects a missing prerequisite after publication; it does not
retroactively enable immutability or undo publication.
An existing release causes failure; there is no overwrite or clobber path. A failed
upload/publication may leave a draft: inspect it and the failed run before recovery.
Do not rebuild or replace published bytes. Any recovery must consume the exact
validated archive, checksum and notes from that run, with explicit maintainer review.

After publication, independently download both assets and repeat the checks above,
including tagged-source template comparison, before accepting the distribution.
As a separate post-merge acceptance step against the actual release, independently
verify GitHub's signed attestation with `gh release verify "$version"` and verify the
downloaded assets with `gh release verify-asset`. PR validation does not call a
real release attestation.
Publication itself performs no host installation, provider qualification or deployment.

## Build and verify qualification bundles

This maintainer procedure builds qualification inputs from source. Operators use
[download and verify a release](#download-and-verify-a-release) instead.

On Linux with Git, Bash, GNU coreutils/find/tar/gzip and the reviewed Go toolchain,
use a clean checkout of the revision under review. The output directory must be
outside the checkout. The script refuses dirty/untracked source and existing output.

```sh
# Run at the repository root; choose a new output directory outside it.
scripts/build-bundle.sh /tmp/media-gateway-bundles
# Extract the reported archive into a new empty directory, then enter it.
mkdir /tmp/media-gateway-extracted
tar -xzf /tmp/media-gateway-bundles/media-gateway-linux-amd64-REVISION.tar.gz \
  -C /tmp/media-gateway-extracted
cd /tmp/media-gateway-extracted
sha256sum --check --strict SHA256SUMS
./media-gateway -version
```

Replace `REVISION` with the full checkout SHA. Require matching revision,
`modified=false` and the reviewed Go toolchain in version output. SHA256SUMS detects
accidental changes; authenticate the source/archive separately through the reviewed
repository. It is not a signature from a trusted publisher.

The explicit bundle contents are `media-gateway`, `config.toml.example`,
`media-gateway.service`, `nginx.conf`, `SHA256SUMS`, `CHANGELOG.md`, `LICENSE`, a short `README.md`,
`smoke-deployment.py`, and offline `docs/` operator/consumer, architecture/security, media qualification,
roadmap and product-contract guidance. Templates contain public example values only; never package
an installed TOML/key or an operator's adapted nginx file. The static Linux amd64
build uses `CGO_ENABLED=0`, `GOAMD64=v1`, normal Go VCS metadata and no custom version
injection. Archive order, ownership and timestamps are normalized. The procedure
is repeatable; byte-for-byte build reproducibility is not claimed.

## First installation

After [release verification](#download-and-verify-a-release), follow
[service account and installation](deployment.md#service-account-and-installation)
for the service identity, permissions, secret provisioning and systemd commands.
Those commands run directly from the extracted release directory using its
binary and top-level templates. Review TOML literals,
private origin and key path locally; retain the separate protected key contract.

Adapt only this service's nginx include: expected Host, dedicated free origin port,
fixed gateway endpoint and log paths. Keep both deny/media servers, the map and log
format. Review effective `nginx -T` locally for inherited routing, error pages,
headers and logging; never publish its potentially sensitive dump. Ensure nginx
proxies only to the gateway endpoint, never the provider or storage. Check unrelated
vhosts/services before and after integration. Run `nginx -t` before every nginx
reload. Do not replace the host's complete nginx configuration.

Run `systemd-analyze verify` on the installed unit, `systemctl daemon-reload`, then
start/enable only the gateway unit. Startup is the TOML/key validator; there is no
validation-only command. Check its journal/listener and run the smoke below before
publishing. The always-present nullable coordinate metadata is private-consumer-only; nginx
must continue denying `/internal/`.

## Upgrade and rollback

Stop on any failed command. These steps assume the reference paths and unit name;
adapt only names that differ on the host. Work in a controlled maintenance window.

1. Verify the new extracted checksums and `./media-gateway -version`. Record the
   current `/usr/local/bin/media-gateway -version` and live process executable
   version (`sudo /proc/$(systemctl show media-gateway.service -p MainPID --value)/exe -version`).
2. Create a new root-only rollback directory outside the repository (`sudo install
   -d -m 0700 /root/media-gateway-rollback-TIMESTAMP`). With `sudo cp -a`, retain the
   current binary, entire `/etc/media-gateway` directory, installed unit and this
   service's adapted nginx include there. Record include location and version;
   preserve ownership/modes and keep the backup private because it contains the key.
   Do not proceed until all copies exist. Keep the previous bundle too.
3. Review template differences against the installed versions. Preserve live TOML,
   key and host adaptations; never install example TOML over live policy. Stage the
   new binary on the same filesystem with `sudo install -o root -g root -m 0755
   ./media-gateway /usr/local/bin/media-gateway.new`, then `sudo mv
   /usr/local/bin/media-gateway.new /usr/local/bin/media-gateway`. This avoids writing
   into the executing binary. Install only intentionally adapted template changes.
4. Recheck config/key metadata and readability as in deployment guidance. Run
   `sudo systemd-analyze verify /etc/systemd/system/media-gateway.service`, then
   `sudo systemctl daemon-reload`. Run `sudo nginx -t` before applying ingress changes.
5. Run `sudo systemctl restart media-gateway.service`; inspect active state and
   sanitized journal/listener. Reload nginx only if its include changed and its
   validation succeeded: `sudo systemctl reload nginx`. TOML/key replacement always
   needs gateway restart; daemon-reload does not apply either and there is no hot reload.
6. Verify installed and live `/proc/.../exe -version` match the intended revision;
   run eligible/private smoke and supplied optional representatives. Preserve the
   backup until acceptance. Check unrelated host services remain healthy.
7. On failure, stop only the gateway, restore the saved binary, config directory
   contents, unit and adapted include with original ownership/modes (`cp -a` from
   the protected backup). Remove any newly introduced task-owned config file only
   after explicit inspection; never clear shared directories. Repeat unit verify,
   daemon-reload and `nginx -t`; reset-failed if startup rate limiting was reached,
   then start the gateway and reload nginx if its include was restored. Verify both
   installed/live version equal the recorded old revision and repeat the smoke.
   Report restoration failures; do not leave a failed rollback described as healthy.

Review the current [configuration schema](configuration.md) before upgrade and
retain the old TOML/key with the old binary for coherent rollback. The optional
`privacy.expose_source_metadata` defaults to false: sensitive catalogue fields and
original capability become null, and originals return 404 without provider fetches.
Enable it deliberately only when source metadata exposure is intended. Original
smoke options require this opt-in; preview privacy checks do not prove original
metadata stripping. Release validation is not deployment or provider qualification.

## Portable smoke interface

Python 3 standard library is sufficient for HTTP checks. No credentials, DNS,
external edge service, public Internet, provider calls or config/key edits are needed. Supply
safe known representatives locally; do not commit their IDs or shell transcripts.

```sh
# Repository: scripts/smoke-deployment.py; bundle: ./smoke-deployment.py.
python3 scripts/smoke-deployment.py \
  --origin "$LOCAL_NGINX_ORIGIN" --host "$EXPECTED_HOST" \
  --eligible-id "$ELIGIBLE_ID" --private-id "$PRIVATE_ID" \
  --near-match-id "$NEAR_MATCH_ID" --outside-root-id "$OUTSIDE_ROOT_ID" \
  --lifecycle-id "$REVOKED_ID" --source-metadata off --video-id "$VIDEO_ID" \
  --gateway-origin "$LOCAL_GATEWAY_ORIGIN" --root "$LOGICAL_ROOT" \
  --collection "$RELATIVE_COLLECTION"
```

Origin must be numeric loopback HTTP with explicit port. Requests do not follow
redirects or ambient proxy settings. The reference Host isolation contract is
required. Output contains fixed check labels only, never bodies, IDs, headers,
credentials, private paths, raw metadata or coordinates. Previews are held in bounded
memory; originals are hashed incrementally with only small range samples retained.
The default original budget is 1 GiB/300 seconds per transfer; increase
`--max-original-bytes` and `--transfer-timeout` deliberately for larger sources.
These are smoke limits, not product size or lifetime ceilings. No originals are saved.

`--source-metadata off|on` declares the expected gateway mode; omission means off.
It never changes configuration. Off requires all five sensitive catalogue keys to
be present/null and checks eligible image/video original GET/HEAD/Range denial.
`--video-id` still checks the poster while off. On requires validated timestamps,
a valid nullable coordinate pair and the exact original capability; neither mode
silently accepts the other. Preview paths and dimensions/duration retain their
checks in both modes.

For an explicitly configured `privacy.expose_source_metadata=true` instance, replace
`--source-metadata off` above with `--source-metadata on` and add
`--image-original --image-sha256 "$IMAGE_SHA256" --video-sha256 "$VIDEO_SHA256"`.
Original-download/hash/long-video options are rejected while off before any requests.
With on, `--image-original` adds full image GET/HEAD; `--video-id` adds poster, full original,
first/suffix byte comparisons, range HEAD, unsatisfiable 416 and malformed 400.
Supply SHA256 values calculated privately from independently retrieved provider
originals to prove source identity; absent hashes are reported as SKIP. Hashes and
response values are never printed. Policy representatives deny on preview and
original, even with malformed Range (authorization must run first).

`--gateway-origin` is a **separate direct numeric loopback** origin for the private
catalogue, accompanied by `--root` and `--collection`. It follows collection and
asset cursors with limit 1, validates the safe projection and fetches supplied
image/video details. `--max-pages` defaults to four per operation (maximum 100).
Choose a collection with multiple assets; absent continuation is SKIP, and an
unfinished scan at the explicit budget is REVIEW, not an exhaustion claim.
nginx-origin checks independently require `/internal/collections` and assets to deny.

For an explicit long-media check, add `--long-video-rate 262144` (paced bytes/second)
and an adequate transfer budget/source size. It repeats the video original,
requires elapsed transfer >65 seconds and compares its full hash to the first GET.
Use a source substantially larger than transport buffers (for example >32 MiB),
check ongoing gateway/nginx activity, and retain the [video worksheet](video-qualification.md)
for cancellation, provider range-404 details and poster privacy/human quality.
A slow receiver alone does not prove upstream activity for the whole interval.

Absent representatives/check options produce explicit SKIP. Lifecycle input proves
a currently denied ID, not a complete revoke/restore transition. Repeat with the
same UUID before revocation, after provider rescan, and after restoration in the
isolated qualification run. For global/scoped convention coverage, rerun with
representatives in each intended rule/root and supply a wrong-root scoped near miss.
Startup of that instance validates publication policy; smoke does not read config/secrets or
infer intended publication policy from the private catalogue.

On the installed Linux host, add `--host-checks --unit "$GATEWAY_UNIT" --user
"$GATEWAY_USER" --listen "$GATEWAY_ENDPOINT" --access-log "$NGINX_ACCESS_LOG"
--error-log "$NGINX_ERROR_LOG"`. Use sufficient privileges to inspect process/socket
ownership and journal/log files (normally `sudo`). Requires systemctl, journalctl,
ss and `/proc`. Checks verify live dedicated UID/GID/groups, zero capabilities,
exact numeric loopback TCP listener, separate nginx/gateway endpoints, journal
routing/events and existing nginx access/error files. Review log ownership/modes,
rotation, actual request entries and absence of sensitive data locally; existence
alone does not prove that logging destinations are correctly wired. Review effective
nginx configuration to prove the fixed upstream and absence of inherited fallback.
Do not copy raw logs/configuration into routine qualification output.

Only `--gateway-outage` authorizes stopping/starting the supplied gateway unit.
It requires host checks and an active service, expects bounded nginx 502/504, always
attempts start in cleanup, then polls eligible GET/HEAD delivery immediately and
every 250 ms within a hard 30-second recovery deadline (including HTTP work).
Transient failures are silent; failure to recover fails the smoke. Active systemd
state alone is not readiness. Normal checks are not retried. Use only in a maintenance
window; interruption normally restores the service, but SIGKILL/power loss cannot
run cleanup. If interrupted, check/start that unit and repeat the normal smoke.
Provider outage/credential-failure evidence is separately obtained by the operator
in an isolated qualification instance: temporarily make only that instance's provider
unavailable, require fixed gateway 502 `media unavailable` within the documented
bound, restore it and repeat eligible/private checks. Never edit shared provider
keys or stop an unrelated provider as part of normal smoke.

## Validation evidence

Record the source revision, bundle checksum, version/template comparison, local baseline,
Code Guard and CI for that revision. Software evidence is not host/provider qualification.

## Isolated deployment validation

Use a reviewed source revision and its exact verified bundle. Check `-version`,
checksums and template identity before preparing an isolated run. Do not rebuild
a substitute bundle on the target host.

1. Inspect existing services/listeners and preserve unrelated host state. Prepare
   temporary unprivileged gateway/config/key copies and isolated nginx. Keep the
   original protected credential source intact. Never enable a persistent gateway
   service or create the public hostname or external edge route.
2. Validate adapted unit/nginx and publication policy startup with representative global
   `public`, image/video-specific and root-scoped `post` conventions. Run the final
   portable smoke against the temporary gateway and nginx, including pagination,
   independently verified image/video source hashes, private/outside-root/near-match
   and revoked IDs. Apply the [video worksheet](video-qualification.md) for complete
   range-404, oversized suffix, long-stream, privacy and revocation evidence.
3. Exercise install/upgrade/rollback using the temporary installation layout and
   retained coherent binary/config/key/template copies. Record installed/live
   revisions before and after each transition; preserve the old bundle. Validate
   config/key permissions and repeat product smoke after upgrade and rollback.
   Restoration must include the old schema/key union when applicable. Do not run
   the persistent `/usr/local` or `/etc` installation commands unadapted.
4. Inspect fixed upstream, Range forwarding, `/internal/` isolation and safe logs.
   Perform outage/auth-failure and service stop/start only with explicit opt-in in
   the isolated instance, always restoring it. Classify unavailable representatives
   and privacy/manual evidence honestly; keep raw provider data out of shared reports.
5. Stop/remove all task-owned services/processes, nginx/config/key copies, downloaded
   source bytes and evidence/rollback directories. Verify no temporary listener,
   installed/enabled qualification service or temporary path remains. Preserve only
   sanitized results and the pre-existing protected credential source. Check
   unrelated services remain healthy. Cleanup is mandatory even after failure.

## Documentation site maintenance

Pages uses **Deploy from a branch → main → /docs**, shared
`stef-k/.github@main`, and `/media-gateway` as base path. If not configured, an
administrator must set that source in repository Settings → Pages. Do not add an
Actions deployment workflow. `docs/index.md` is the sole homepage (`permalink: /`).
The local `documentation` layout is only an alias to the shared `default` layout;
no shared CSS/markup is copied. Relative Markdown links are rewritten by the
GitHub Pages-supported `jekyll-relative-links` plugin.

When publishing documentation, verify the published homepage, shared styling, navigation and each
internal link under `/media-gateway`, including anchors. Verify root README ↔ docs
homepage navigation and inspect rendered content for stale/private material.
A local build does not establish that the published site renders correctly.

## Original image qualification

Use these source-identity checks when validating a deployment;
preview privacy evidence does not establish original metadata stripping.

Record sanitized evidence, without IDs, private paths, credentials or raw metadata:

- deployed Immich version and exact gateway revision;
- dedicated key permissions: asset.read, asset.view, asset.download;
- representative eligible JPEG and RAW/ARW or HEIC if available (record absence);
- provider original GET status/type/length, gateway byte identity by hash and length;
- preserved source type, GET/HEAD framing and immediate HEAD body closure;
- same-ID lifecycle/path revocation and known-private original denial with no fallback;
- nginx original ingress, unknown Host/private/provider/malformed route denial and
  caller Range/conditional stripping (full 200, no Accept-Ranges);
- bounded, sanitized provider outage/auth failure and absence of path/credential/
  source-metadata leaks in response headers and routine logs.

Embedded EXIF/GPS in authorized original **body bytes** is intentional source
preservation, not a metadata-minimal derivative claim. Use the separately qualified
preview for web/privacy requirements. Never inspect/rewrite originals in the gateway.
