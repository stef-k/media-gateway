#!/usr/bin/env python3
"""Behavioral smoke-tool regressions using synthetic HTTP responses, not host proof."""

import contextlib
import hashlib
import http.server
import json
import importlib.util
import io
import pathlib
import signal
import subprocess
import sys
import threading
import time
import unittest
import urllib.parse
from types import SimpleNamespace
from unittest.mock import patch


# Dynamic script imports must not dirty a clean bundle checkout.
sys.dont_write_bytecode = True

# Deliberately synthetic UUIDs; no qualification library identifiers.
ELIGIBLE = "00000000-0000-4000-8000-000000000001"
PRIVATE = "00000000-0000-4000-8000-000000000002"
VIDEO = "00000000-0000-4000-8000-000000000003"
SOURCE = bytes(range(128))
SCRIPT = pathlib.Path(__file__).with_name("smoke-deployment.py")


class Origin(http.server.BaseHTTPRequestHandler):
    """Supply only the response contract being tested, including selected violations."""

    def do_GET(self):
        if self.server.defect == "stall":
            threading.Event().wait(0.2)
            return
        eligible = self.path == f"/media/{ELIGIBLE}/preview"
        allowed = eligible and self.headers["Host"] == "smoke.example" and self.command in ("GET", "HEAD")
        body = b"image" if allowed else b"not found\n"
        if self.server.defect == "private-leak" and PRIVATE in self.path:
            allowed, body = True, b"synthetic-private-marker"
        self.send_response(200 if allowed else 404)
        self.send_header("Content-Type", "image/jpeg" if allowed else "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Cache-Control", "public" if self.server.defect == "cache" else "no-store")
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    do_HEAD = do_GET
    do_POST = do_GET
    do_PUT = do_GET
    do_PATCH = do_GET
    do_DELETE = do_GET
    do_OPTIONS = do_GET

    def log_message(self, *args):
        """Keep synthetic request details out of test output."""


class ProductOrigin(Origin):
    """Serve bounded catalogue pages and source/range responses to the real CLI."""

    def asset(self, identifier):
        """Use only the shipped catalogue schema and synthetic source metadata."""
        return {"id": identifier, "media_type": "video" if identifier == VIDEO else "image",
                "root": "images", "collection_path": "trip/public", "filename": "example",
                "width": None, "height": None, "duration_ms": None,
                "file_created_at": "2026-01-01T00:00:00Z", "local_date_time": "2026-01-01T00:00:00Z",
                "latitude": None, "longitude": None,
                "preview_path": f"/media/{identifier}/preview", "original_path": f"/media/{identifier}/original"}

    def do_GET(self):
        if self.path.startswith("/internal/") and self.headers.get("Host") != "smoke.example":
            target = urllib.parse.urlsplit(self.path)
            query = urllib.parse.parse_qs(target.query)
            if target.path in ("/internal/collections", "/internal/assets"):
                key = target.path.rsplit("/", 1)[1]
                item = {"root": "images", "collection_path": "trip/public"} if key == "collections" else self.asset(ELIGIBLE)
                payload = {key: [item], "next_cursor": None if "cursor" in query else "synthetic-cursor"}
                if self.server.defect == "catalogue-leak":
                    item["provider_path"] = "synthetic-private-marker"
            else:
                payload = self.asset(target.path.rsplit("/", 1)[1])
            self.reply(200, "application/json", json.dumps(payload).encode())
            return
        if self.headers.get("Host") != "smoke.example":
            return super().do_GET()
        if self.path == f"/media/{VIDEO}/preview":
            self.reply(200, "image/jpeg", b"poster")
        elif self.path in (f"/media/{VIDEO}/original", f"/media/{ELIGIBLE}/original"):
            self.original()
        else:
            super().do_GET()

    def original(self):
        """Model full, first/suffix, unsatisfiable and malformed video requests."""
        video = VIDEO in self.path
        headers = {"Accept-Ranges": "bytes"} if video else {}
        status, body = 200, SOURCE
        value = self.headers.get("Range") if video else None
        if value == "bytes=0-31":
            status, body = 206, SOURCE[:32]
            headers["Content-Range"] = "bytes 0-31/128"
        elif value == "bytes=-32":
            status, body = 206, SOURCE[-32:]
            headers["Content-Range"] = "bytes 96-127/128"
        elif value == "bytes=128-":
            status, body = 416, b""
            headers["Content-Range"] = "bytes */128"
        elif value:
            self.reply(400, "text/plain; charset=utf-8", b"invalid range\n")
            return
        if self.server.defect == "range-bytes" and status == 206:
            body = b"x" * len(body)
        if self.server.defect == "range-framing" and status == 206:
            headers["Content-Range"] = "bytes 1-32/128"
        self.reply(status, "video/mp4" if video else "image/jpeg", body, headers)

    def reply(self, status, content_type, body, headers=None):
        """Write GET/HEAD framing without logging identifiers or response data."""
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        for key, value in (headers or {}).items():
            self.send_header(key, value)
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    do_HEAD = do_GET


class SmokeContract(unittest.TestCase):
    """The CLI must pass the contract and fail unsafe responses without leaking bytes."""

    def run_smoke(self, defect, product=False, extra=()):
        with http.server.ThreadingHTTPServer(("127.0.0.1", 0), ProductOrigin if product else Origin) as server:
            server.defect = defect
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                options = []
                if product:
                    options = ["--image-original", "--video-id", VIDEO,
                               "--image-sha256", hashlib.sha256(SOURCE).hexdigest(),
                               "--video-sha256", hashlib.sha256(SOURCE).hexdigest(),
                               "--gateway-origin", f"http://127.0.0.1:{server.server_port}",
                               "--root", "images", "--collection", "trip/public", "--lifecycle-id", PRIVATE]
                return subprocess.run([
                    "python3", str(SCRIPT), "--origin", f"http://127.0.0.1:{server.server_port}",
                    "--host", "smoke.example", "--eligible-id", ELIGIBLE, "--private-id", PRIVATE,
                    "--near-match-id", PRIVATE, "--outside-root-id", PRIVATE,
                ] + options + list(extra), capture_output=True, text=True, timeout=15)
            finally:
                server.shutdown()
                thread.join()

    def test_product_contract_and_rejected_unsafe_evidence(self):
        """The CLI verifies source identity/ranges/pagination and fails closed on defects."""
        result = self.run_smoke(None, product=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("PASS video range bytes", result.stdout)
        self.assertIn("PASS bounded collection/asset pages", result.stdout)
        self.assertNotIn("SKIP continuation", result.stdout)
        for defect, extra in (("range-bytes", ()), ("range-framing", ()),
                              ("catalogue-leak", ()), (None, ("--max-original-bytes", "64")),
                              (None, ("--image-sha256", "0" * 64))):
            with self.subTest(defect=defect, extra=extra):
                result = self.run_smoke(defect, product=True, extra=extra)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("synthetic-private-marker", result.stdout + result.stderr)
                self.assertNotIn(VIDEO, result.stdout + result.stderr)

    def test_recovery_deadline_interrupts_stalled_http(self):
        """The total recovery budget interrupts HTTP, not just the retry sleeps."""
        spec = importlib.util.spec_from_file_location("smoke", SCRIPT)
        smoke = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(smoke)
        with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Origin) as server:
            server.defect = "stall"
            thread = threading.Thread(target=server.handle_request)
            thread.start()
            previous = signal.signal(signal.SIGALRM, smoke.interrupted)
            try:
                args = SimpleNamespace(origin=f"http://127.0.0.1:{server.server_port}", host="smoke.example")
                with self.assertRaisesRegex(RuntimeError, "request deadline"):
                    smoke.request(args, "/", deadline=time.monotonic() + 0.05)
                self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))
            finally:
                signal.signal(signal.SIGALRM, previous)
                thread.join()

    def test_safe_responses_pass(self):
        result = self.run_smoke(None)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("SKIP host inspection", result.stdout)
        self.assertIn("SKIP disruptive", result.stdout)

    def test_cache_and_private_exposure_fail_without_dumping_data(self):
        for defect in ("cache", "private-leak"):
            with self.subTest(defect=defect):
                result = self.run_smoke(defect)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("synthetic-private-marker", result.stdout + result.stderr)
                self.assertNotIn(PRIVATE, result.stdout + result.stderr)


class OutageRecovery(unittest.TestCase):
    """Exercise stop/start recovery with deterministic time and eligible HTTP results."""

    def setUp(self):
        spec = importlib.util.spec_from_file_location("smoke", SCRIPT)
        self.smoke = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.smoke)
        self.args = SimpleNamespace(host_checks=True, unit="example.service", eligible_id=ELIGIBLE)
        self.now = 0
        self.output = io.StringIO()
        self.commands = self.enterContext(patch.object(self.smoke, "command", return_value="active"))
        self.enterContext(patch.object(self.smoke.time, "monotonic", side_effect=lambda: self.now))
        self.sleep = self.enterContext(patch.object(self.smoke.time, "sleep", side_effect=self.advance))
        self.enterContext(contextlib.redirect_stdout(self.output))

    def advance(self, seconds):
        """Advance virtual time without wall-clock sleeps."""
        self.now += seconds

    def image(self, method="GET"):
        """Return a complete eligible response, including HEAD framing."""
        return 200, {"content-type": "image/jpeg", "content-length": "5",
                     "cache-control": "no-store", "x-content-type-options": "nosniff"}, b"image" if method == "GET" else b""

    def test_delayed_post_start_delivery(self):
        responses = [(502, {}, b""), (502, {}, b"private-marker"),
                     ConnectionRefusedError("private-marker"), self.image(), self.image("HEAD")]
        with patch.object(self.smoke, "request", side_effect=responses) as request:
            self.smoke.outage_check(self.args)
        self.assertEqual([call.args[1] for call in self.commands.call_args_list], ["is-active", "stop", "start"])
        self.assertEqual(self.now, 0.5)
        self.assertEqual([call.kwargs.get("deadline") for call in request.call_args_list], [None, 30, 30, 30, 30])
        self.assertNotIn("private-marker", self.output.getvalue())
        self.assertNotIn(ELIGIBLE, self.output.getvalue())
        self.assertIn("PASS bounded gateway outage", self.output.getvalue())

    def test_unrecovered_delivery_fails_at_deadline(self):
        with patch.object(self.smoke, "request", return_value=(502, {}, b"private-marker")):
            with self.assertRaisesRegex(RuntimeError, "did not recover within 30 seconds"):
                self.smoke.outage_check(self.args)
        self.assertEqual(self.now, 30)
        self.assertTrue(all(call.args[0] == 0.25 for call in self.sleep.call_args_list))
        self.assertEqual(self.output.getvalue(), "")

    def test_immediate_recovery_needs_no_sleep(self):
        with patch.object(self.smoke, "request", side_effect=[(502, {}, b""), self.image(), self.image("HEAD")]):
            self.smoke.outage_check(self.args)
        self.sleep.assert_not_called()

    def test_failed_outage_still_starts_and_normal_failure_is_not_retried(self):
        with patch.object(self.smoke, "request", return_value=(200, {}, b"")):
            with self.assertRaisesRegex(RuntimeError, "outage did not fail closed"):
                self.smoke.outage_check(self.args)
        self.commands.assert_called_with("systemctl", "start", self.args.unit)
        with patch.object(self.smoke, "request", return_value=(502, {}, b"")) as request:
            with self.assertRaisesRegex(RuntimeError, "eligible image failed"):
                self.smoke.image_check(self.args)
        request.assert_called_once()
        self.sleep.assert_not_called()


if __name__ == "__main__":
    unittest.main()
