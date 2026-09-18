#!/usr/bin/env python3
"""Bounded local-origin checks; print classifications, never response or log data."""

import argparse
import hashlib
import http.client
import ipaddress
import json
import pathlib
import pwd
import re
import signal
import subprocess
import sys
import time
import urllib.parse


def require(condition, label):
    """Fail with a fixed diagnostic rather than potentially private input."""
    if not condition:
        raise RuntimeError(label)


def command(*args):
    """Capture host tools privately and bound execution; never echo tool errors."""
    result = subprocess.run(args, capture_output=True, text=True, timeout=40)
    require(result.returncode == 0, "host command failed (check privileges/tools locally)")
    return result.stdout.strip()


def request(args, path, method="GET", host=None, deadline=None, headers=None,
            stream=False, origin=None, rate=0):
    """Use numeric local HTTP only, no proxies, redirects, DNS or saved image files."""
    consumer = origin is not None
    origin = urllib.parse.urlsplit(origin or args.origin)
    connection = http.client.HTTPConnection(origin.hostname, origin.port, timeout=75)
    budget = args.transfer_timeout if stream else 80
    if deadline is not None:
        budget = min(budget, deadline - time.monotonic())
    require(budget > 0, "eligible recovery deadline exceeded")
    signal.setitimer(signal.ITIMER_REAL, budget)
    try:
        connection.request(method, path, headers={**(headers or {}), "Host": host or (origin.netloc if consumer else args.host)})
        response = connection.getresponse()
        headers = {}
        for key, value in response.getheaders():
            key = key.lower()
            require(key not in headers, "duplicate response header")
            headers[key] = value
        # A socket timeout is inactivity-based; the outer process alarm bounds total work.
        body = read_original(response, args, rate) if stream else response.read(16 * 1024 * 1024 + 1)
        require(stream or len(body) <= 16 * 1024 * 1024, "response exceeds image bound")
        return response.status, headers, body
    finally:
        connection.close()
        signal.setitimer(signal.ITIMER_REAL, 0)


def read_original(response, args, rate):
    """Hash bounded source bytes incrementally, retaining only small range samples."""
    started = time.monotonic()
    digest, count, first, last = hashlib.sha256(), 0, b"", b""
    while True:
        chunk = response.read(65536)
        if not chunk:
            break
        count += len(chunk)
        require(count <= args.max_original_bytes, "original exceeds smoke byte budget")
        digest.update(chunk)
        first = (first + chunk)[:32]
        last = (last + chunk)[-32:]
        if rate and count < int(response.getheader("Content-Length", "0")):
            time.sleep(max(0, count / rate - (time.monotonic() - started)))
    return {"length": count, "sha256": digest.hexdigest(), "first": first,
            "last": last, "seconds": time.monotonic() - started}


def media_headers(headers):
    """Require constructed public headers and reject provider/header leakage."""
    require(headers.get("cache-control") == "no-store", "no-store failed")
    require(headers.get("x-content-type-options") == "nosniff", "nosniff failed")
    allowed = {"server", "date", "connection", "content-type", "content-length",
               "cache-control", "x-content-type-options", "accept-ranges", "content-range"}
    require(set(headers) <= allowed, "unexpected media headers")
    length = headers.get("content-length", "")
    require(length.isdecimal(), "media length failed")
    return int(length)


def original_check(args, asset, kind, expected_hash=None, rate=0):
    """Check complete original GET/HEAD with optional independently supplied hash."""
    path = f"/media/{asset}/original"
    status, headers, body = request(args, path, stream=True, rate=rate)
    length = media_headers(headers)
    require(status == 200 and length > 0 and body["length"] == length, "original framing failed")
    content_type = headers.get("content-type", "")
    require(re.fullmatch(kind + r"/[a-zA-Z0-9!#$&^_.+-]+", content_type) or
            (kind == "video" and content_type == "application/mxf"), "original type failed")
    require("content-range" not in headers, "full original has range framing")
    require(headers.get("accept-ranges") == ("bytes" if kind == "video" else None), "original range capability failed")
    if expected_hash:
        require(body["sha256"] == expected_hash.lower(), "original source hash mismatch")
    else:
        print("SKIP independent source identity (supply original SHA256)")
    status, head, empty = request(args, path, "HEAD", headers={"Range": "bytes=0-1"} if kind == "image" else None)
    require(status == 200 and empty == b"" and media_headers(head) == length, "original HEAD failed")
    for field in ("content-type", "content-length", "accept-ranges", "content-range"):
        require(head.get(field) == headers.get(field), "original HEAD framing differs")
    if rate:
        require(body["seconds"] > 65, "long original did not exceed 65 seconds")
    print("PASS original GET/HEAD framing and bounded streaming")
    body["content_type"] = content_type
    return body


def video_ranges(args, source):
    """Compare representative ranges against full-source samples and validate 416."""
    path = f"/media/{args.video_id}/original"
    total = source["length"]
    end = min(31, total - 1)
    cases = [("bytes=0-31", 0, end, source["first"]),
             ("bytes=-32", max(0, total - 32), total - 1, source["last"])]
    for value, first, last, expected in cases:
        for method in ("GET", "HEAD"):
            status, headers, body = request(args, path, method, headers={"Range": value, "If-Range": '"ignored"'})
            require(status == 206 and media_headers(headers) == last - first + 1, "video range status/length failed")
            require(headers.get("content-type") == source["content_type"], "video range type differs")
            require(headers.get("content-range") == f"bytes {first}-{last}/{total}" and
                    headers.get("accept-ranges") == "bytes", "video range interval failed")
            require(body == (expected if method == "GET" else b""), "video range bytes failed")
    for method in ("GET", "HEAD"):
        status, headers, body = request(args, path, method, headers={"Range": f"bytes={total}-"})
        require(status == 416 and media_headers(headers) == 0 and body == b"", "video unsatisfied range failed")
        require(headers.get("content-range") == f"bytes */{total}" and
                headers.get("accept-ranges") == "bytes", "video 416 framing failed")
    status, headers, body = request(args, path, headers={"Range": "bytes=0-1,3-4"})
    require(status == 400 and body == b"invalid range\n", "malformed video range failed")
    media_headers(headers)
    print("PASS video range bytes, HEAD, 206, 416 and malformed range")


def catalogue_pages(args, path, key, selectors):
    """Follow gateway continuations within a finite operator-controlled page budget."""
    cursor = None
    continued = False
    for _ in range(args.max_pages):
        query = {**selectors, "limit": "1"}
        if cursor is not None:
            query["cursor"] = cursor
            continued = True
        status, headers, body = request(args, path + "?" + urllib.parse.urlencode(query), origin=args.gateway_origin)
        require(status == 200 and len(body) <= 512 * 1024, "catalogue response failed")
        media_headers(headers)
        require(headers.get("content-type") == "application/json", "catalogue type failed")
        page = json.loads(body)
        require(set(page) == {key, "next_cursor"} and isinstance(page[key], list) and len(page[key]) <= 1, "catalogue page shape failed")
        for item in page[key]:
            if key == "collections":
                require(set(item) == {"root", "collection_path"}, "collection projection failed")
            else:
                catalogue_asset(item)
                require(item["root"] == args.root and item["collection_path"] == args.collection, "collection selector failed")
        cursor = page["next_cursor"]
        require(cursor is None or isinstance(cursor, str) and 0 < len(cursor) <= 2048, "catalogue cursor failed")
        if cursor is None:
            break
    if not continued:
        print("SKIP continuation absent in supplied catalogue")
    if cursor is not None:
        print("REVIEW catalogue traversal stopped at explicit page budget")


def catalogue_asset(item):
    """Check only the public consumer projection; never print private response values."""
    fields = {"id", "media_type", "root", "collection_path", "filename", "width", "height",
              "duration_ms", "file_created_at", "local_date_time", "latitude", "longitude",
              "preview_path", "original_path"}
    require(set(item) == fields and item["media_type"] in ("image", "video"), "asset projection failed")
    require(isinstance(item["collection_path"], str) and not item["collection_path"].startswith("/"), "absolute collection path exposed")
    for variant in ("preview", "original"):
        require(item[variant + "_path"] == f"/media/{item['id']}/{variant}", "asset capability failed")


def product_checks(args):
    """Run optional final-product checks while reporting missing evidence explicitly."""
    if args.image_original:
        original_check(args, args.eligible_id, "image", args.image_sha256)
    else:
        print("SKIP image original (use --image-original)")
    if args.video_id:
        image_check(argparse.Namespace(**{**vars(args), "eligible_id": args.video_id}))
        source = original_check(args, args.video_id, "video", args.video_sha256)
        video_ranges(args, source)
        if args.long_video_rate:
            original_check(args, args.video_id, "video", source["sha256"], args.long_video_rate)
            print("PASS active video transfer exceeds 65 seconds")
        else:
            print("SKIP long video transfer (use --long-video-rate)")
    else:
        print("SKIP video representatives")
    if args.gateway_origin:
        catalogue_pages(args, "/internal/collections", "collections", {})
        catalogue_pages(args, "/internal/assets", "assets", {"root": args.root, "collection": args.collection})
        for asset in (args.eligible_id, args.video_id):
            if asset:
                status, headers, body = request(args, f"/internal/assets/{asset}", origin=args.gateway_origin)
                require(status == 200, "catalogue detail failed")
                media_headers(headers)
                item = json.loads(body)
                catalogue_asset(item)
                require(item["id"].lower() == asset.lower(), "catalogue detail identity failed")
        print("PASS bounded collection/asset pages and detail")
    else:
        print("SKIP direct-loopback catalogue (supply gateway origin/root/collection)")


def image_check(args, deadline=None):
    """Check GET bytes and independently authorized HEAD framing/privacy headers."""
    path = f"/media/{args.eligible_id}/preview"
    for method in ("GET", "HEAD"):
        status, headers, body = request(args, path, method, deadline=deadline)
        require(status == 200, "eligible image failed")
        require(headers.get("content-type") in ("image/jpeg", "image/webp"), "image type failed")
        length = headers.get("content-length", "")
        require(length.isdecimal() and 0 < int(length) <= 16 * 1024 * 1024, "image length failed")
        require(headers.get("cache-control") == "no-store", "no-store failed")
        require(headers.get("x-content-type-options") == "nosniff", "nosniff failed")
        require(not set(headers) & {"location", "set-cookie", "etag", "content-encoding", "content-range", "content-disposition"}, "unexpected image headers")
        require(len(body) == (int(length) if method == "GET" else 0), "image body length failed")
    require(deadline is None or time.monotonic() < deadline, "eligible recovery deadline exceeded")
    print("PASS eligible GET/HEAD headers and byte length")


def denial_checks(args):
    """Cover private policy representatives and the reference nginx route boundary."""
    for asset in (args.private_id, args.near_match_id, args.outside_root_id, args.lifecycle_id):
        if asset:
            for variant in ("preview", "original"):
                status, headers, body = request(args, f"/media/{asset}/{variant}", headers={"Range": "malformed"})
                require(status == 404 and body == b"not found\n", "policy denial failed")
                require(headers.get("cache-control") == "no-store", "denial no-store failed")
    paths = ("/internal/collections", "/internal/assets", f"/internal/assets/{args.eligible_id}", "/api", "/api/assets", "/", "/photos", "/albums", "/search", "/unknown", "/media", "/media/", f"/media/{args.eligible_id}/fullsize", f"/media//{args.eligible_id}/preview", f"/media/%2e%2e/media/{args.eligible_id}/preview")
    for path in paths:
        require(request(args, path)[0] == 404, "public route isolation failed")
    for method in ("POST", "PUT", "PATCH", "DELETE", "OPTIONS"):
        require(request(args, f"/media/{args.eligible_id}/preview", method)[0] == 404, "method isolation failed")
    require(request(args, f"/media/{args.eligible_id}/preview", host="invalid.example")[0] == 404, "Host isolation failed")
    print("PASS policy, public routes, methods and wrong Host")
    if not args.lifecycle_id:
        print("SKIP lifecycle-denied representative (same-ID revoke/restore remains operator work)")
    if not args.near_match_id or not args.outside_root_id:
        print("SKIP absent optional near-match/outside-root representatives")


def host_checks(args):
    """Inspect the live systemd process and its sockets without dumping private state."""
    require(args.unit and args.user and args.listen and args.access_log and args.error_log, "host checks require unit/user/listen/access-log/error-log")
    account = pwd.getpwnam(args.user)
    require(account.pw_uid != 0, "service identity must be unprivileged")
    pid = command("systemctl", "show", args.unit, "--property=MainPID", "--value")
    require(pid.isdecimal() and int(pid) > 0, "service has no live PID")
    status = pathlib.Path(f"/proc/{pid}/status").read_text()
    fields = dict(line.split(":", 1) for line in status.splitlines() if ":" in line)
    require(set(fields["Uid"].split()) == {str(account.pw_uid)}, "process UID mismatch")
    require(set(fields["Gid"].split()) == {str(account.pw_gid)}, "process GID mismatch")
    require(set(fields["Groups"].split()) <= {str(account.pw_gid)}, "unexpected supplementary groups")
    for field in ("CapEff", "CapPrm", "CapBnd", "CapAmb"):
        require(int(fields[field], 16) == 0, "process capabilities are not empty")
    sockets = command("ss", "-H", "-ltnp")
    endpoints = [line.split()[3] for line in sockets.splitlines() if f"pid={pid}," in line]
    require(endpoints == [args.listen], "process listener mismatch or insufficient ss privileges")
    require(command("systemctl", "show", args.unit, "-p", "StandardOutput", "--value") == "journal", "stdout must use journal")
    require(command("systemctl", "show", args.unit, "-p", "StandardError", "--value") == "journal", "stderr must use journal")
    require(command("journalctl", "-u", args.unit, "-n", "1", "-o", "json", "--quiet"), "unit journal is empty")
    for name in (args.access_log, args.error_log):
        require(pathlib.Path(name).is_file(), "nginx log file missing")
    require(pathlib.Path(args.access_log).stat().st_size > 0, "nginx access log is empty")
    print("PASS live identity, capabilities, listener, journal and nginx log files")
    print("REVIEW effective nginx upstream/inheritance and log ownership/privacy locally; see README")


def wait_for_recovery(args):
    """Poll eligible GET/HEAD immediately, then every 250 ms, for at most 30 seconds."""
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            image_check(args, deadline=deadline)
            return
        except (RuntimeError, OSError, http.client.HTTPException):
            # Transient responses and transport errors may contain private values.
            pass
        remaining = deadline - time.monotonic()
        if remaining > 0:
            time.sleep(min(0.25, remaining))
    raise RuntimeError("eligible delivery did not recover within 30 seconds")


def outage_check(args):
    """Explicit gateway stop only; always attempt restoration, including on interrupt."""
    require(args.host_checks, "gateway outage requires host checks")
    require(command("systemctl", "is-active", args.unit) == "active", "outage requires active service")
    try:
        command("systemctl", "stop", args.unit)
        require(request(args, f"/media/{args.eligible_id}/preview")[0] in (502, 504), "outage did not fail closed")
    finally:
        command("systemctl", "start", args.unit)
    wait_for_recovery(args)
    print("PASS bounded gateway outage and restored eligible delivery")


def validate_origin(value):
    """Reject non-loopback origins before any optional consumer request."""
    origin = urllib.parse.urlsplit(value)
    require(origin.scheme == "http" and origin.hostname and ipaddress.ip_address(origin.hostname).is_loopback and origin.port and not origin.username and not origin.password and origin.path in ("", "/") and not origin.query and not origin.fragment, "origin must be a numeric loopback HTTP origin with explicit port")


def main():
    """Parse operator inputs before any network request or optional host mutation."""
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("origin", "host", "eligible-id", "private-id"):
        parser.add_argument("--" + name, required=True)
    for name in ("near-match-id", "outside-root-id", "lifecycle-id", "video-id", "image-sha256", "video-sha256", "gateway-origin", "root", "collection", "unit", "user", "listen", "access-log", "error-log"):
        parser.add_argument("--" + name)
    parser.add_argument("--host-checks", action="store_true")
    parser.add_argument("--gateway-outage", action="store_true", help="DISRUPTIVE: stop/start supplied gateway unit, then verify recovery")
    parser.add_argument("--image-original", action="store_true")
    parser.add_argument("--max-original-bytes", type=int, default=1024 * 1024 * 1024, help="smoke budget, not a product size limit")
    parser.add_argument("--transfer-timeout", type=int, default=300, help="total smoke transfer budget in seconds")
    parser.add_argument("--long-video-rate", type=int, default=0, help="opt-in paced video read, bytes/second; requires >65s source")
    parser.add_argument("--max-pages", type=int, default=4, help="maximum pages per catalogue operation")
    args = parser.parse_args()
    require(args.max_original_bytes > 0 and args.transfer_timeout > 0 and args.long_video_rate >= 0 and 1 <= args.max_pages <= 100, "invalid smoke budget")
    require(not args.long_video_rate or args.video_id, "long video requires video representative")
    for digest in (args.image_sha256, args.video_sha256):
        require(not digest or re.fullmatch(r"[a-fA-F0-9]{64}", digest), "invalid expected SHA256")
    require(not args.image_sha256 or args.image_original, "image SHA256 requires image original check")
    require(not args.video_sha256 or args.video_id, "video SHA256 requires video representative")
    if args.gateway_origin:
        require(args.root and args.collection, "catalogue requires root and collection")
        validate_origin(args.gateway_origin)
    else:
        require(not args.root and not args.collection, "catalogue selectors require gateway origin")
    validate_origin(args.origin)
    origin = urllib.parse.urlsplit(args.origin)
    require(re.fullmatch(r"[A-Za-z0-9.-]+(?::[0-9]+)?", args.host) and args.host != "invalid.example", "invalid expected Host")
    for asset in (args.eligible_id, args.private_id, args.near_match_id, args.outside_root_id, args.lifecycle_id, args.video_id):
        require(not asset or re.fullmatch(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}", asset), "representatives must be UUIDv4")
    if args.listen:
        endpoint = urllib.parse.urlsplit("http://" + args.listen)
        require(ipaddress.ip_address(endpoint.hostname).is_loopback and endpoint.port, "gateway listener must be numeric loopback")
        require((endpoint.hostname, endpoint.port) != (origin.hostname, origin.port), "nginx and gateway endpoints must differ")
    require(not args.unit or re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", args.unit), "unit must be a service name")
    image_check(args)
    denial_checks(args)
    product_checks(args)
    if args.host_checks:
        host_checks(args)
    else:
        print("SKIP host inspection (use --host-checks on installed host)")
    if args.gateway_outage:
        outage_check(args)
    else:
        print("SKIP disruptive outage check (explicit opt-in required)")


def interrupted(signum, frame):
    """Convert timeout/termination into cleanup-aware failure."""
    raise RuntimeError("request deadline or interruption")


if __name__ == "__main__":
    signal.signal(signal.SIGALRM, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    try:
        main()
    except RuntimeError as error:
        print("FAIL " + str(error), file=sys.stderr)
        sys.exit(1)
    except (Exception, KeyboardInterrupt):
        # Do not print exceptions: OS/network errors may contain operator input.
        print("FAIL smoke check; inspect installed state privately (no response/log dump)", file=sys.stderr)
        sys.exit(1)
