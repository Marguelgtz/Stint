#!/usr/bin/env python3
"""Small localhost HTTP forwarder used to isolate Hermes reasoning routes.

Hermes associates custom-provider request overrides with a provider's base URL.
The CP1 box therefore runs one forwarder per effort level; both forwarders
reach the same NInfer listener but have distinct base URLs in Hermes config.
"""

import http.server
import json
import os
import sys
import urllib.error
import urllib.request


LISTEN = int(sys.argv[1])
UPSTREAM = sys.argv[2].rstrip("/")
LOGFILE = sys.argv[3]
TAG = sys.argv[4] if len(sys.argv) > 4 else str(LISTEN)
os.makedirs(os.path.dirname(LOGFILE) or ".", exist_ok=True)


class Forwarder(http.server.BaseHTTPRequestHandler):
    def _forward(self, body: bytes) -> None:
        headers = {
            key: value
            for key, value in self.headers.items()
            if key.lower() not in {"host", "content-length", "connection"}
        }
        request = urllib.request.Request(
            UPSTREAM + self.path,
            data=body,
            headers=headers,
            method=self.command,
        )
        try:
            response = urllib.request.urlopen(request, timeout=300)
            payload = response.read()
            self.send_response(response.status)
            for key, value in response.headers.items():
                if key.lower() in {"transfer-encoding", "connection", "content-length"}:
                    continue
                self.send_header(key, value)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except urllib.error.HTTPError as error:
            payload = error.read()
            self.send_response(error.code)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except Exception as error:  # pragma: no cover - exercised on a box
            self.send_response(502)
            self.send_header("Content-Length", "0")
            self.end_headers()
            print(f"proxy error: {error}", flush=True)

        if body and self.path.endswith("/chat/completions"):
            try:
                payload = json.loads(body)
                record = {
                    "phase": TAG,
                    "model": payload.get("model"),
                    "reasoning_effort": payload.get("reasoning_effort"),
                }
                with open(LOGFILE, "a", encoding="utf-8") as stream:
                    stream.write(json.dumps(record) + "\n")
            except Exception:
                pass

    def do_GET(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler contract
        self._forward(b"")

    def do_POST(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler contract
        length = int(self.headers.get("content-length", "0"))
        self._forward(self.rfile.read(length) if length else b"")

    def log_message(self, *_args) -> None:
        return


if __name__ == "__main__":
    http.server.ThreadingHTTPServer(("127.0.0.1", LISTEN), Forwarder).serve_forever()
