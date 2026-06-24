#!/usr/bin/env python3
"""swarm-proxy.py — transparent API proxy with per-session parameter injection.

Sits between claude CLI and DeepSeek API, injecting temperature/top_p/max_tokens
per session without affecting the MCP tool loop. Also logs real token usage.

Usage:
    SWARM_PROXY_CONFIG=/path/to/proxy-config.json python3 swarm-proxy.py [PORT]
    Default port: 8443
"""

import json
import os
import sys
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler

import urllib.request
import urllib.error

CONFIG_FILE = os.environ.get("SWARM_PROXY_CONFIG", "")
UPSTREAM = os.environ.get("SWARM_PROXY_UPSTREAM", "https://api.deepseek.com/anthropic")
CONFIG = {}
TOKEN_LOG = {}
CONFIG_LOCK = threading.Lock()


def load_config():
    """Live-reload session→param mapping from JSON file."""
    global CONFIG
    if not CONFIG_FILE or not os.path.exists(CONFIG_FILE):
        return
    try:
        with open(CONFIG_FILE) as f:
            with CONFIG_LOCK:
                CONFIG = json.load(f)
    except Exception:
        pass


class ProxyHandler(BaseHTTPRequestHandler):
    """HTTP handler that proxies Anthropic-format requests with param injection."""

    def do_request(self):
        load_config()

        # Parse target path
        path = self.path.lstrip("/")
        if path == "health":
            self.send_json(200, {"status": "ok", "sessions": list(CONFIG.keys()),
                                  "tokens": TOKEN_LOG})
            return
        if path == "tokens":
            self.send_json(200, TOKEN_LOG)
            return

        # Read request body
        content_len = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_len) if content_len > 0 else b""

        # Inject per-session parameters into /messages requests
        session_id = self.headers.get("x-session-id", "")
        body_json = json.loads(body) if body else {}
        if "messages" in self.path and body_json:
            params = CONFIG.get(session_id, {})
            for key in ("temperature", "top_p", "max_tokens"):
                if key in params:
                    body_json.setdefault(key, params[key])
            # Let session config override thinking mode.
            # "thinking": {"type": "enabled", "budget_tokens": N} → deep reasoning
            # "thinking": {"type": "disabled"} → fast/no reasoning
            # Not set → whatever claude CLI sends (respects --effort flag)
            body = json.dumps(body_json).encode()

        # Forward to upstream
        url = f"{UPSTREAM}/{path}"
        req = urllib.request.Request(
            url, data=body, method=self.command
        )
        skip_headers = {"host", "content-length", "x-session-id"}
        for key, val in self.headers.items():
            if key.lower() not in skip_headers:
                req.add_header(key, val)
        req.add_header("Content-Type", "application/json")

        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                resp_body = resp.read()
                self.send_response(resp.status)
                for key, val in resp.getheaders():
                    if key.lower() not in ("transfer-encoding",):
                        self.send_header(key, val)
                self.end_headers()
                self.wfile.write(resp_body)

                # Log token usage from response
                if session_id and "messages" in self.path and resp_body:
                    try:
                        data = json.loads(resp_body)
                        usage = data.get("usage", {})
                        tin = usage.get("input_tokens", usage.get("prompt_tokens", 0))
                        tout = usage.get("output_tokens", usage.get("completion_tokens", 0))
                        if tin or tout:
                            TOKEN_LOG[session_id] = {"tokens_in": tin, "tokens_out": tout}
                    except Exception:
                        pass
        except urllib.error.HTTPError as e:
            err_body = e.read()
            self.send_response(e.code)
            self.end_headers()
            self.wfile.write(err_body)
        except Exception as e:
            self.send_json(502, {"error": str(e)})

    def send_json(self, status, data):
        body = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    do_GET = do_POST = do_PUT = do_DELETE = do_request


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 0  # 0 = random port
    server = HTTPServer(("127.0.0.1", port), ProxyHandler)
    actual_port = server.socket.getsockname()[1]
    print(f"SWARM_PROXY_PORT={actual_port}", flush=True)
    print(f"[swarm-proxy] listening on 127.0.0.1:{actual_port}, upstream={UPSTREAM}", file=sys.stderr, flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
        print("[swarm-proxy] stopped", file=sys.stderr, flush=True)


if __name__ == "__main__":
    main()
