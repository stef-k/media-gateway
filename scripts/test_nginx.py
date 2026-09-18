#!/usr/bin/env python3
"""Exercise the shipped ingress template using local nginx and a synthetic upstream."""

import http.client
import http.server
import os
import pathlib
import shutil
import signal
import socket
import subprocess
import tempfile
import threading
import time
import unittest


class Upstream(http.server.BaseHTTPRequestHandler):
    """Capture forwarded requests; publication policy remains the Go handler's job."""

    def do_GET(self):
        self.server.requests.append((self.path, dict(self.headers)))
        self.server.release.wait(timeout=10)
        self.send_response(200)
        self.send_header("Content-Type", "image/jpeg")
        self.send_header("Content-Length", "7")
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(b"source!")

    do_HEAD = do_GET

    def log_message(self, *args):
        """Keep synthetic request details quiet."""


@unittest.skipUnless(shutil.which("nginx"), "local nginx is unavailable")
class Ingress(unittest.TestCase):
    """Prove raw routes and header isolation through a real isolated nginx process."""

    def test_image_routes(self):
        with tempfile.TemporaryDirectory(prefix="media-gateway-nginx-") as directory:
            with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Upstream) as upstream:
                upstream.requests = []
                upstream.release = threading.Event()
                upstream.release.set()
                thread = threading.Thread(target=upstream.serve_forever)
                thread.start()
                try:
                    self.run_nginx(pathlib.Path(directory), upstream)
                finally:
                    upstream.release.set()
                    upstream.shutdown()
                    thread.join()

    def run_nginx(self, directory, upstream):
        """Adapt only installation values, validate, then stop only our child process."""
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        template = (pathlib.Path(__file__).resolve().parents[1] / "deploy/nginx.conf").read_text()
        template = template.replace("127.0.0.1:8089", f"127.0.0.1:{port}")
        template = template.replace("127.0.0.1:2290", f"127.0.0.1:{upstream.server_port}")
        template = template.replace("/var/log/nginx/", str(directory) + "/")
        config = directory / "nginx.conf"
        config.write_text(f"daemon off; master_process off; pid {directory}/nginx.pid;\n"
                          f"error_log {directory}/process.log;\nevents {{}}\nhttp {{\n{template}\n}}\n")
        command = [shutil.which("nginx"), "-p", str(directory), "-c", str(config)]
        subprocess.run(command + ["-t"], check=True, capture_output=True)
        process = subprocess.Popen(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            deadline = time.monotonic() + 3
            while True:
                try:
                    with socket.create_connection(("127.0.0.1", port), timeout=0.1):
                        break
                except OSError:
                    if process.poll() is not None or time.monotonic() >= deadline:
                        self.fail("isolated nginx did not start")
                    time.sleep(0.02)
            self.check_requests(port, upstream)
            self.check_concurrency(port, upstream)
            self.check_burst(port, upstream, process)
        finally:
            process.terminate()
            process.wait(timeout=3)
        log = (directory / "media-gateway.access.log").read_text()
        for route in ("preview", "original", "denied"):
            self.assertIn(f"route={route}", log)
        self.assertNotIn("caller-marker", log)
        self.assertIn("request_limit=REJECTED", log)
        self.assertIn("connection_limit=REJECTED", log)
        self.assertNotIn("limiting requests", (directory / "media-gateway.error.log").read_text())

    def check_concurrency(self, port, upstream):
        """Hold 32 admitted responses; the next request and cheap denials stay local."""
        route = "/media/12345678-1234-4234-8234-123456789abc/original"
        connections = []
        before = len(upstream.requests)
        upstream.release.clear()
        try:
            for index in range(32):
                connection = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
                connections.append(connection)
                connection.request("GET", route, headers={"Host": "media.example.com"})
                deadline = time.monotonic() + 3
                while len(upstream.requests) != before + index + 1:
                    self.assertLess(time.monotonic(), deadline, "held request was not admitted")
                    time.sleep(0.005)
            self.check_denials(port, upstream)
            connection = http.client.HTTPConnection("127.0.0.1", port, timeout=3)
            try:
                connection.request("HEAD", route, headers={"Host": "media.example.com"})
                response = connection.getresponse()
                self.assertEqual(response.status, 429)
                response.read()
                self.assertEqual(len(upstream.requests), before + 32)
            finally:
                connection.close()
        finally:
            upstream.release.set()
            for connection in connections:
                response = connection.getresponse()
                self.assertEqual(response.status, 200)
                response.read()
                connection.close()

    def check_denials(self, port, upstream):
        """Even with admission saturated, invalid requests bypass limits and upstream."""
        route = "/media/12345678-1234-4234-8234-123456789abc/preview"
        before = len(upstream.requests)
        for method, path, host in (("POST", route, "media.example.com"),
                                   ("GET", route, "wrong.example"),
                                   ("GET", route, None),
                                   ("GET", route + "/extra", "media.example.com"),
                                   ("GET", "/internal/assets", "media.example.com"),
                                   ("GET", "/api/assets", "media.example.com")):
            connection = http.client.HTTPConnection("127.0.0.1", port, timeout=3)
            try:
                # HTTP/1.0 permits absent Host; HTTP/1.1 requires it at parse time.
                connection._http_vsn = 10
                connection._http_vsn_str = "HTTP/1.0"
                connection.putrequest(method, path, skip_host=True)
                if host is not None:
                    connection.putheader("Host", host)
                connection.endheaders()
                response = connection.getresponse()
                self.assertEqual(response.status, 404)
                response.read()
            finally:
                connection.close()
        self.assertEqual(len(upstream.requests), before)

    def check_burst(self, port, upstream, process):
        """Queue a burst while our nginx child is stopped; keep shipped limits intact."""
        connections = []
        before = len(upstream.requests)
        process.send_signal(signal.SIGSTOP)
        os.waitpid(process.pid, os.WUNTRACED)
        try:
            for index in range(128):
                connection = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
                connections.append(connection)
                variant = "preview" if index % 2 else "original"
                connection.request("GET", f"/media/12345678-1234-4234-8234-123456789abc/{variant}",
                                   headers={"Host": "media.example.com"})
        finally:
            process.send_signal(signal.SIGCONT)
        try:
            statuses = []
            for connection in connections:
                response = connection.getresponse()
                statuses.append(response.status)
                response.read()
            self.assertIn(429, statuses)
            self.assertLess(len(upstream.requests) - before, len(connections))
            self.assertEqual(len(upstream.requests) - before, statuses.count(200))
            self.assertTrue(set(statuses) <= {200, 429})
            self.check_denials(port, upstream)
        finally:
            for connection in connections:
                connection.close()

    def check_requests(self, port, upstream):
        """Canonical GET/HEAD succeed; malformed/unsupported traffic never reaches upstream."""
        asset = "12345678-1234-4234-8234-123456789abc"
        for variant in ("preview", "original"):
            route = f"/media/{asset}/{variant}"
            cases = [(method, route, "media.example.com", True) for method in ("GET", "HEAD")]
            cases += [("GET", route + "?edited=true", "media.example.com", True)]
            cases += [("POST", route, "media.example.com", False), ("GET", route, "wrong.example", False)]
            for path in (route + "/extra", route + "/", route.replace("/media/", "/media//"),
                         route.replace("/media/", "/media/../media/"), route.replace(asset, "%31" + asset[1:]),
                         route.replace(asset, "invalid"), f"/media/{asset}/fullsize", "/media", "/internal/assets",
                         "/internal/collections", f"/api/assets/{asset}/original"):
                cases.append(("GET", path, "media.example.com", False))
            for method, path, host, allowed in cases:
                with self.subTest(method=method, path=path, host=host):
                    before = len(upstream.requests)
                    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=3)
                    headers = {name: "caller-marker" for name in ("Authorization", "Cookie", "x-api-key",
                               "Range", "If-Range", "If-None-Match", "If-Modified-Since",
                               "If-Match", "If-Unmodified-Since", "Upgrade", "X-Caller")}
                    connection.request(method, path, body=b"unused", headers={**headers, "Host": host})
                    response = connection.getresponse()
                    body = response.read()
                    connection.close()
                    self.assertEqual(response.status, 200 if allowed else 404)
                    self.assertEqual(len(upstream.requests), before + int(allowed))
                    if allowed:
                        self.assertEqual(body, b"" if method == "HEAD" else b"source!")
                        self.assertEqual(response.getheader("Cache-Control"), "no-store")
                        self.assertIsNone(response.getheader("Accept-Ranges"))
                        forwarded = {key.lower(): value for key, value in upstream.requests[-1][1].items()}
                        for name in (*headers, "Content-Length"):
                            if name == "Range" and variant == "original":
                                self.assertEqual(forwarded.get("range"), "caller-marker")
                            else:
                                self.assertNotIn(name.lower(), forwarded)


if __name__ == "__main__":
    unittest.main()
