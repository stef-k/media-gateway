# Linux bundle and deployment smoke

#33 supplies these tools; acceptance on the exact bundle/host is still required
before #33/#5 close. #31/#32 templates are accepted. No GitHub Release or production
hostname/Cloudflare cutover is performed by this procedure.

PR CI may retain the verified Linux amd64 bundle as the
`media-gateway-linux-amd64` CI artifact for qualification without Go on the target.
It contains only the verified `.tar.gz`. CI artifacts are not signed releases and
are not the production distribution mechanism.

## Build and verify

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
`media-gateway.service`, `nginx.conf`, `SHA256SUMS`, `LICENSE`, this `README.md`,
`smoke-deployment.py`, and offline `docs/` release/deployment/configuration/logging/toolchain/
consumer-API guidance. Templates contain public example values only; never package
an installed TOML/key or an operator's adapted nginx file. The static Linux amd64
build uses `CGO_ENABLED=0`, `GOAMD64=v1`, normal Go VCS metadata and no custom version
injection. Archive order, ownership and timestamps are normalized. The procedure
is repeatable; byte-for-byte build reproducibility is not claimed.

## First installation

Use [deployment](https://github.com/stef-k/media-gateway/blob/main/docs/deployment.md)
in the repository, or `docs/deployment.md` in the
bundle, for the accepted identity, permissions, secret provisioning and systemd
commands. From an extracted bundle, substitute `./media-gateway` for the build
output and the top-level template names for `deploy/...`; no Go toolchain is needed
on the target. Verify checksums/version before installing. Review TOML literals,
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
publishing. The opt-in #26 coordinate capability is private-consumer-only; nginx
must continue denying `/internal/`.

## Upgrade and rollback

Stop on any failed command. These steps assume the accepted paths and unit name;
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

## Portable smoke interface

Python 3 standard library is sufficient for HTTP checks. No credentials, DNS,
Cloudflare, public Internet, provider calls or config/key edits are needed. Supply
safe known representatives locally; do not commit their IDs or shell transcripts.

```sh
# Repository: scripts/smoke-deployment.py; bundle: ./smoke-deployment.py.
python3 scripts/smoke-deployment.py \
  --origin "$LOCAL_NGINX_ORIGIN" --host "$EXPECTED_HOST" \
  --eligible-id "$ELIGIBLE_ID" --private-id "$PRIVATE_ID" \
  --near-match-id "$NEAR_MATCH_ID" --outside-root-id "$OUTSIDE_ROOT_ID"
```

Origin must be numeric loopback HTTP with explicit port. Requests do not follow
redirects or ambient proxy settings. The reference Host isolation contract is
required. Output contains fixed check labels only, never bodies, IDs, headers,
credentials, private paths, raw metadata or coordinates. Images are held only in
bounded memory. Checks cover eligible GET/HEAD type/length/no-store/nosniff, fixed
private denial, optional near-match/outside-root denial, private/provider/UI/unknown
routes, crafted paths, unsupported methods and wrong Host. Missing representatives
are explicitly skipped, not passed. This is not EXIF or human visual qualification.

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
attempts start in cleanup, then verifies eligible recovery. Use only in a maintenance
window; interruption normally restores the service, but SIGKILL/power loss cannot
run cleanup. If interrupted, check/start that unit and repeat the normal smoke.
Provider outage/credential-failure evidence is separately obtained by the operator
in an isolated qualification instance: temporarily make only that instance's provider
unavailable, require fixed gateway 502 `media unavailable` within the documented
bound, restore it and repeat eligible/private checks. Never edit shared provider
keys or stop an unrelated provider as part of normal smoke.

## Acceptance evidence

Record exact source/bundle SHA, checksum/version/template comparison, normal Go
checks, Code Guard and exact-head CI. After source review, qualify the exact bundle
and smoke on M6: installed/live version, normal and optional representatives,
identity/listener, effective upstream and logging ownership/privacy, safe outage
and restoration where approved, plus upgrade/rollback evidence. Accepted #31/#32
host results remain valid but do not prove new #33 tooling. Update tracker #1 and
#5 only after acceptance/merge. Production cutover and #30 remain separate work.
