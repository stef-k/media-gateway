#!/usr/bin/env python3
"""Behavioral smoke-tool regressions using synthetic HTTP responses, not host proof."""

import contextlib
import http.server
import importlib.util
import io
import pathlib
import signal
import subprocess
import sys
import threading
import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch


# Dynamic script imports must not dirty a clean bundle checkout.
sys.dont_write_bytecode = True

# Deliberately synthetic UUIDs; no qualification library identifiers.
ELIGIBLE = "00000000-0000-4000-8000-000000000001"
PRIVATE = "00000000-0000-4000-8000-000000000002"
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


class SmokeContract(unittest.TestCase):
    """The CLI must pass the contract and fail unsafe responses without leaking bytes."""

    def run_smoke(self, defect):
        with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Origin) as server:
            server.defect = defect
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                return subprocess.run([
                    "python3", str(SCRIPT), "--origin", f"http://127.0.0.1:{server.server_port}",
                    "--host", "smoke.example", "--eligible-id", ELIGIBLE, "--private-id", PRIVATE,
                    "--near-match-id", PRIVATE, "--outside-root-id", PRIVATE,
                ], capture_output=True, text=True, timeout=15)
            finally:
                server.shutdown()
                thread.join()

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
