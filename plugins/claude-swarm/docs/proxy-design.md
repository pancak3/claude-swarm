# Swarm API Proxy — Implementation Plan

## Architecture

```
claude CLI (session s1) ──→ http://127.0.0.1:PROXY_PORT ──→ https://api.deepseek.com/anthropic
claude CLI (session s2) ──→ http://127.0.0.1:PROXY_PORT ──→ https://api.deepseek.com/anthropic
claude CLI (session s3) ──→ http://127.0.0.1:PROXY_PORT ──→ https://api.deepseek.com/anthropic
                                  │
                          swarm-proxy.py
                          (Flask, ~120 lines)
                                  │
                    Reads session→param mapping from
                    RUN_DIR/proxy-config.json
```

## Files

| File | Action | Purpose |
|------|--------|---------|
| `scripts/swarm-proxy.py` | Create | The proxy server |
| `scripts/swarm-orchestrate.sh` | Modify | Start proxy, pass port to spawn |
| `scripts/swarm-spawn.sh` | Modify | Set ANTHROPIC_BASE_URL to proxy |

## swarm-proxy.py — Core Design

```python
# Dependencies: flask, requests (already available or pip install)

from flask import Flask, request, Response
import json, requests, threading, os, sys

app = Flask(__name__)
CONFIG = {}       # session_id → {temperature, top_p, max_tokens}
TOKEN_LOG = {}    # session_id → {tokens_in, tokens_out, cost}
UPSTREAM = "https://api.deepseek.com/anthropic"
CONFIG_FILE = os.environ.get("SWARM_PROXY_CONFIG", "")

def load_config():
    """Reload session→param mapping from JSON file."""
    global CONFIG
    if CONFIG_FILE and os.path.exists(CONFIG_FILE):
        with open(CONFIG_FILE) as f:
            CONFIG = json.load(f)

@app.route('/anthropic/<path:subpath>', methods=['GET','POST','PUT','DELETE'])
@app.route('/v1/<path:subpath>', methods=['GET','POST','PUT','DELETE'])
def proxy(subpath=""):
    load_config()  # live-reload config each request
    
    session_id = request.headers.get('x-session-id', '')
    params = CONFIG.get(session_id, {})
    
    body = request.get_json(silent=True) or {}
    
    # Only inject params into /messages (chat completions)
    if 'messages' in request.path and request.method == 'POST':
        for key in ('temperature', 'top_p', 'max_tokens'):
            if key in params and key not in body:
                body[key] = params[key]
    
    # Forward to upstream
    url = f"{UPSTREAM}/{subpath}"
    headers = {k: v for k, v in request.headers.items() 
               if k.lower() not in ('host', 'content-length')}
    
    try:
        resp = requests.request(
            method=request.method, url=url, headers=headers,
            json=body, stream=False, timeout=120
        )
    except Exception as e:
        return {"error": str(e)}, 502
    
    # Log token usage from response
    if request.method == 'POST' and 'messages' in request.path:
        try:
            data = resp.json()
            usage = data.get('usage', {})
            tin = usage.get('input_tokens', usage.get('prompt_tokens', 0))
            tout = usage.get('output_tokens', usage.get('completion_tokens', 0))
            if session_id:
                TOKEN_LOG[session_id] = {'tokens_in': tin, 'tokens_out': tout}
        except:
            pass
    
    return Response(resp.content, status=resp.status_code, 
                    headers=dict(resp.headers))

@app.route('/health')
def health():
    return {"status": "ok", "sessions": list(CONFIG.keys()),
            "tokens": TOKEN_LOG}

@app.route('/tokens')
def tokens():
    """Return token usage for all sessions (consumed by orchestrator)."""
    return TOKEN_LOG

if __name__ == '__main__':
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8443
    app.run(host='127.0.0.1', port=port, threaded=True)
```

## proxy-config.json (written by orchestrator)

```json
{
  "s1": {"temperature": 0.3, "top_p": 0.95},
  "s2": {"temperature": 0.7, "top_p": 0.95},
  "s3": {"temperature": 0.5, "top_p": 0.95},
  "s4": {"temperature": 0.2, "top_p": 0.95},
  "s5": {"temperature": 0.3, "top_p": 0.95},
  "s6": {"temperature": 0.5, "top_p": 0.95},
  "s7": {"temperature": 0.7, "top_p": 0.95},
  "s8": {"temperature": 0.2, "top_p": 0.95}
}
```

## Integration into swarm-orchestrate.sh

```
Before:  start bus → start dashboard → spawn sessions → wait
After:   start proxy → start bus → spawn sessions → wait → kill proxy

Changes:
1. After RUN_DIR creation, write proxy-config.json with per-session temperatures
2. Start swarm-proxy.py on SWARM_PROXY_PORT (random, like bus)
3. Pass SWARM_PROXY_PORT to swarm-spawn.sh via env
4. Add proxy PID to cleanup trap (kill after bus)
```

## Integration into swarm-spawn.sh

```
Changes:
1. If SWARM_PROXY_PORT is set, export:
   ANTHROPIC_BASE_URL="http://127.0.0.1:${SWARM_PROXY_PORT}"
2. Pass x-session-id header:
   ANTHROPIC_HEADERS="x-session-id: ${SESSION_ID}" (if supported)
   OR add to claude invocation if CLI supports --header
```

## Temperature Diversity Strategy

| Perspective | Temperature | Rationale |
|-------------|-------------|-----------|
| correctness | 0.2 | Conservative, precise, logical |
| simplicity  | 0.4 | Clarity over creativity |
| performance | 0.8 | Creative optimization, unexpected solutions |
| security    | 0.1 | Most conservative, thorough |

Override via `SWARM_TEMPERATURES` env var:
```bash
SWARM_TEMPERATURES="0.3,0.5,0.7,0.2"  # correctness,simplicity,performance,security
```

## Token Tracking (improved)

With the proxy, we get real token counts from the API response (`usage.prompt_tokens`, `usage.completion_tokens`) rather than parsing `claude -p` text output. The proxy exposes `/tokens` endpoint for the orchestrator to query after sessions complete.

## What Doesn't Change

- `claude` CLI tool loop (MCP register/propose/vote/read_round) — unchanged
- Session management (`--session-id`) — unchanged
- System prompt templates — unchanged
- Bus, dashboard, cleanup — unchanged
- All existing shell scripts except spawn.sh + orchestrator for proxy lifecycle

## Risks

| Risk | Mitigation |
|------|-----------|
| Proxy crash takes down all sessions | Sessions fail-fast, orchestrator detects 0 registrations |
| DeepSeek ignores temperature | Test first; if ignored, proxy still works as pass-through |
| x-session-id header not passed by claude CLI | Fallback: use config file + API key as session identifier |
| Extra latency (~5ms per request) | Negligible vs API latency (~2-5s) |
| flask/requests dependencies | Check at startup, print clear error if missing |

## Implementation Order

1. `swarm-proxy.py` — the proxy server itself (standalone, testable)
2. `swarm-orchestrate.sh` — start/kill proxy, write config
3. `swarm-spawn.sh` — set ANTHROPIC_BASE_URL
4. Integration test — run a 2-session swarm, verify different temps via proxy logs
