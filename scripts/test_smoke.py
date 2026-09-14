#!/usr/bin/env python3
"""Behavioral smoke-tool regressions using synthetic HTTP responses, not host proof."""

import http.server
import pathlib
import subprocess
import threading
import unittest


# Deliberately synthetic UUIDs; no qualification library identifiers.
ELIGIBLE = "00000000-0000-4000-8000-000000000001"
PRIVATE = "00000000-0000-4000-8000-000000000002"
SCRIPT = pathlib.Path(__file__).with_name("smoke-deployment.py")


class Origin(http.server.BaseHTTPRequestHandler):
    """Supply only the response contract being tested, including selected violations."""

    def do_GET(self):
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


if __name__ == "__main__":
    unittest.main()
