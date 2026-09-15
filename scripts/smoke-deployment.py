#!/usr/bin/env python3
"""Bounded local-origin checks; print classifications, never response or log data."""

import argparse
import http.client
import ipaddress
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


def request(args, path, method="GET", host=None, deadline=None):
    """Use numeric local HTTP only, no proxies, redirects, DNS or saved image files."""
    origin = urllib.parse.urlsplit(args.origin)
    connection = http.client.HTTPConnection(origin.hostname, origin.port, timeout=75)
    budget = 80 if deadline is None else min(80, deadline - time.monotonic())
    require(budget > 0, "eligible recovery deadline exceeded")
    signal.setitimer(signal.ITIMER_REAL, budget)
    try:
        connection.request(method, path, headers={"Host": host or args.host})
        response = connection.getresponse()
        headers = {}
        for key, value in response.getheaders():
            key = key.lower()
            require(key not in headers, "duplicate response header")
            headers[key] = value
        # A socket timeout is inactivity-based; the outer process alarm bounds total work.
        body = response.read(16 * 1024 * 1024 + 1)
        require(len(body) <= 16 * 1024 * 1024, "response exceeds image bound")
        return response.status, headers, body
    finally:
        connection.close()
        signal.setitimer(signal.ITIMER_REAL, 0)


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
    for asset in (args.private_id, args.near_match_id, args.outside_root_id):
        if asset:
            status, headers, body = request(args, f"/media/{asset}/preview")
            require(status == 404 and body == b"not found\n", "policy denial failed")
            require(headers.get("cache-control") == "no-store", "denial no-store failed")
    paths = ("/internal/assets", f"/internal/assets/{args.eligible_id}", "/api", "/api/assets", "/", "/photos", "/albums", "/search", "/unknown", "/media", "/media/", f"/media/{args.eligible_id}/fullsize", f"/media//{args.eligible_id}/preview", f"/media/%2e%2e/media/{args.eligible_id}/preview")
    for path in paths:
        require(request(args, path)[0] == 404, "public route isolation failed")
    for method in ("POST", "PUT", "PATCH", "DELETE", "OPTIONS"):
        require(request(args, f"/media/{args.eligible_id}/preview", method)[0] == 404, "method isolation failed")
    require(request(args, f"/media/{args.eligible_id}/preview", host="invalid.example")[0] == 404, "Host isolation failed")
    print("PASS policy, public routes, methods and wrong Host")
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


def main():
    """Parse operator inputs before any network request or optional host mutation."""
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("origin", "host", "eligible-id", "private-id"):
        parser.add_argument("--" + name, required=True)
    for name in ("near-match-id", "outside-root-id", "unit", "user", "listen", "access-log", "error-log"):
        parser.add_argument("--" + name)
    parser.add_argument("--host-checks", action="store_true")
    parser.add_argument("--gateway-outage", action="store_true", help="DISRUPTIVE: stop/start supplied gateway unit, then verify recovery")
    args = parser.parse_args()
    origin = urllib.parse.urlsplit(args.origin)
    require(origin.scheme == "http" and origin.hostname and ipaddress.ip_address(origin.hostname).is_loopback and origin.port and not origin.username and not origin.password and origin.path in ("", "/") and not origin.query and not origin.fragment, "origin must be a numeric loopback HTTP origin with explicit port")
    require(re.fullmatch(r"[A-Za-z0-9.-]+(?::[0-9]+)?", args.host) and args.host != "invalid.example", "invalid expected Host")
    for asset in (args.eligible_id, args.private_id, args.near_match_id, args.outside_root_id):
        require(not asset or re.fullmatch(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}", asset), "representatives must be UUIDv4")
    if args.listen:
        endpoint = urllib.parse.urlsplit("http://" + args.listen)
        require(ipaddress.ip_address(endpoint.hostname).is_loopback and endpoint.port, "gateway listener must be numeric loopback")
        require((endpoint.hostname, endpoint.port) != (origin.hostname, origin.port), "nginx and gateway endpoints must differ")
    require(not args.unit or re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", args.unit), "unit must be a service name")
    image_check(args)
    denial_checks(args)
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
