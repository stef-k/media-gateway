#!/usr/bin/env python3
"""Exercise the shipped ingress template using local nginx and a synthetic upstream."""

import http.client
import http.server
import pathlib
import shutil
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
                thread = threading.Thread(target=upstream.serve_forever)
                thread.start()
                try:
                    self.run_nginx(pathlib.Path(directory), upstream)
                finally:
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
        finally:
            process.terminate()
            process.wait(timeout=3)
        log = (directory / "media-gateway.access.log").read_text()
        for route in ("preview", "original", "denied"):
            self.assertIn(f"route={route}", log)
        self.assertNotIn("caller-marker", log)

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
