---
title: "Video qualification worksheet"
---

# Video qualification worksheet

#41 passed this gate before PR #47 merged at
`7053ac1f297b97be6daacca7ff450b442cb689e7`. Reuse this worksheet as the
video portion of [#42 final product qualification](release.md#final-disposable-qualification-42).
Run only after software/docs review of the exact unmerged PR head and retained
CI bundle. Synthetic tests do not qualify a host/provider. All infrastructure is
temporary; production cutover belongs to `stef-k/server-migration#10`.

## Prepare an isolated run

- Verify the PR head with `gh pr view <PR> --json headRefOid` and compare
  `git rev-parse HEAD`, the verified CI bundle revision, and `media-gateway -version`.
- Use a temporary unprivileged gateway on `127.0.0.1:2290` and isolated nginx on
  `127.0.0.1:8089`, adapting the shipped templates and running `nginx -t` first.
  Inspect existing listeners/services first; preserve unrelated host services.
- Copy only the preserved dedicated credential into the protected temporary run.
  Key permissions remain `asset.read + asset.view + asset.download`; no admin/write.
  Do not enable/install a persistent gateway service or publish the public hostname.
- Select a currently eligible MP4 and, if safely available, a second source/container,
  a known private video, and an eligible image. Keep IDs, private paths, headers and
  downloaded originals in restricted temporary files, outside Git and published logs.
- Confirm provider VIDEO metadata, active lifecycle, dimensions/duration and version.

## Byte and framing commands

The following is an operator worksheet, not an unattended deployment script.
Set `video_id`, `image_id`, `private_id`, `gateway` (the isolated nginx origin),
`provider` (the private origin), and `evidence` (a mode-0700 temporary directory)
locally. Use the configured nginx Host below. `provider_curl_config` names a mode-0600
curl config containing the dedicated x-api-key header, created from the protected
key without putting its value in command arguments/history. Remove that copy during
cleanup. Do not print provider headers: they can contain private filenames.

```sh
# Full exact sources: compare bytes, length and hash locally.
curl --fail --silent --show-error --config "$provider_curl_config" \
  -D "$evidence/provider.headers" -o "$evidence/provider.bin" \
  "$provider/api/assets/$video_id/original"
curl --fail --silent --show-error -H 'Host: media.example.com' \
  -D "$evidence/gateway.headers" -o "$evidence/gateway.bin" \
  "$gateway/media/$video_id/original"
cmp "$evidence/provider.bin" "$evidence/gateway.bin"
sha256sum "$evidence/provider.bin" "$evidence/gateway.bin"
wc -c < "$evidence/provider.bin"

# Example first range; repeat at both endpoints for middle, clipped tail,
# open-ended and suffix ranges calculated from the actual source length.
curl --silent --show-error --config "$provider_curl_config" \
  -H 'Range: bytes=0-1023' -D "$evidence/provider-range.headers" \
  -o "$evidence/provider-range.bin" "$provider/api/assets/$video_id/original"
curl --silent --show-error -H 'Host: media.example.com' \
  -H 'Range: bytes=0-1023' -D "$evidence/gateway-range.headers" \
  -o "$evidence/gateway-range.bin" "$gateway/media/$video_id/original"
dd if="$evidence/provider.bin" of="$evidence/expected.bin" bs=1 count=1024 status=none
cmp "$evidence/expected.bin" "$evidence/provider-range.bin"
cmp "$evidence/expected.bin" "$evidence/gateway-range.bin"

# HEAD must match GET framing while returning no body.
curl --silent --show-error --head -H 'Host: media.example.com' \
  -H 'Range: bytes=0-1023' "$gateway/media/$video_id/original"

# Set total to the validated full-source byte count. Immich 3.2.0 returns 404
# for this unsatisfiable Range; keep its error body private, never relay it.
curl --silent --show-error --config "$provider_curl_config" \
  -H "Range: bytes=$total-" -D "$evidence/provider-unsatisfied.headers" \
  -o "$evidence/provider-unsatisfied.body" "$provider/api/assets/$video_id/original"

# Gateway disambiguates with one no-Range original GET, validates its 200
# length and closes the probe body. Expect gateway zero-body 416,
# Content-Range: bytes */total, Content-Length: 0 and Accept-Ranges: bytes.
curl --silent --show-error -H 'Host: media.example.com' \
  -H "Range: bytes=$total-" -D "$evidence/416.headers" \
  -o "$evidence/416.body" "$gateway/media/$video_id/original"
test ! -s "$evidence/416.body"
curl --silent --show-error --head -H 'Host: media.example.com' \
  -H "Range: bytes=$total-" "$gateway/media/$video_id/original"

# Eligible malformed/multiple ranges -> 400; private video -> 404 first.
curl --silent --show-error -H 'Host: media.example.com' \
  -H 'Range: bytes=0-1,3-4' "$gateway/media/$video_id/original" -o /dev/null -w '%{http_code}\n'
curl --silent --show-error -H 'Host: media.example.com' \
  -H 'Range: bytes=0-1' -H 'Range: bytes=3-4' \
  "$gateway/media/$video_id/original" -o /dev/null -w '%{http_code}\n'
curl --silent --show-error -H 'Host: media.example.com' \
  -H 'Range: bytes=0-1' "$gateway/media/$private_id/original" -o /dev/null -w '%{http_code}\n'

# Image Range remains full 200, without Accept-Ranges.
curl --silent --show-error --head -H 'Host: media.example.com' \
  -H 'Range: bytes=0-1' "$gateway/media/$image_id/original"

# Choose a rate/source size that takes >65 seconds while making continuous progress.
curl --fail --silent --show-error --limit-rate 256k -H 'Host: media.example.com' \
  -o "$evidence/slow.bin" -w 'seconds=%{time_total}\n' \
  "$gateway/media/$video_id/original"
cmp "$evidence/provider.bin" "$evidence/slow.bin"
```

Check direct provider and gateway no-range 200, first/middle/tail/open/suffix 206,
provider unsatisfiable 404, gateway synthesized 416 and HEAD. Confirm exactly one
same-endpoint no-Range disambiguation GET for range 404, no extra request for 206,
and no third request. Expect oversized suffix -> provider 404 -> one validated
no-Range original -> gateway full-representation 206. Compare its entire body
with the saved source and check `Content-Range: bytes 0-(total-1)/total` and
`Content-Length: total`; HEAD has identical framing and zero body. Equal suffix
uses the same recovery if the provider returns 404. All other probe bodies close
without streaming. A second provider 404 remains public 404; smaller satisfiable
suffixes and other satisfiable range-404 cases remain sanitized 502 contradictions.
Direct strictly validated provider 416 remains supported but is not the expected
Immich 3.2.0 qualification path. Validate exact intervals, positive totals, types and
lengths, including clipped ends and suffixes larger than the source. No redirects,
playback substitutions, provider validators/disposition/cookies or error bodies may
escape. If Immich original does not satisfy this contract, stop qualification and report the discrepancy before acceptance.

## Authorization, poster, ingress and lifetime

- Compare fixed provider thumbnail `?size=preview` and gateway preview GET/HEAD:
  direct 200 JPEG/WebP, positive length <=16 MiB, usable poster and no unexpected
  sensitive embedded metadata. Check browser seeking for a playable source.
- Capture a catalogue video reference, revoke its convention by moving/rescanning
  the same UUID, prove preview/original 404, then restore/rescan and prove success.
- Temporarily simulate credential/permission failure and provider outage in the
  isolated run: bounded sanitized 502, recovery after restoration, no leakage.
- Confirm nginx forwards Range only to original, strips preview Range, If-Range,
  all conditionals, credentials/cookies and Upgrade. Test wrong Host, `/internal/`,
  provider-looking/encoded paths and POST denial. Inspect sanitized logs locally.
- Complete an active original through gateway **and nginx** for >65 seconds with
  exact hash/length. Cancel a separate live transfer and verify prompt upstream
  release and quiet safe logs. Deterministic local tests cover read/write stalls
  and forced close after the bounded shutdown drain.

## Mandatory cleanup

Stop and remove only the temporary processes/unit/config/key copies created for
this run, including the curl credential copy and downloaded source evidence.
Preserve sanitized qualification results and the separately protected original
credential source under its existing root-only policy. Verify:

```sh
# Both commands must show no remaining qualification listener.
ss -ltnp 'sport = :2290'
ss -ltnp 'sport = :8089'
# Neither an enabled nor installed qualification gateway service may remain.
systemctl is-enabled media-gateway.service
systemctl show media-gateway.service -p LoadState -p ActiveState
```

Also verify all recorded temporary paths are removed. Record cleanup alongside
provider/HTTP/lifetime evidence in the PR. No persistent Media Gateway deployment
or `media.stefk.me`/Cloudflare route may remain from this qualification.
