---
name: Stage 0: CDP Connection Test Learning
description: Replayable CDP connection verification for X/Twitter browser access before the social workflow runs.
type: project
---

## EXECUTION WORKFLOW (EXACT MODE)

### OPTIMAL PATH

1. **Execute the CDP connection test script**
   - arguments: `cd {{WORKSPACE_PATH}} && python3 runs/iteration-30/manish/execution/step-1/test_cdp_connection.py '{{TWITTER_CDP_URL}}'`
   - prerequisites:
     - `{{TWITTER_CDP_URL}}` is set to the live Chrome CDP endpoint for the Twitter browser session.
     - Use the resolved IP form, not `host.docker.internal`, when the endpoint is exposed through Docker networking.
   - outputs:
     - Writes `runs/iteration-30/manish/execution/step-1/connection_test.json`
     - Expected success payload: `{"connected": true, "twitter_visible": true, "cdp_url": "...", "error": null}`
   - on_error:
     - `connected=false`: CDP endpoint is wrong, Chrome is not running, or the browser target cannot be reached.
     - `twitter_visible=false`: the browser connected, but the page did not confirm the X/Twitter UI after navigation to `https://x.com/home`.
     - `Host header is not an IP or localhost`: do not pass `host.docker.internal` directly; use the resolved IP URL instead.

2. **Validate the written JSON**
   - arguments: `cat {{WORKSPACE_PATH}}/runs/iteration-30/manish/execution/step-1/connection_test.json`
   - prerequisites: step 1 has completed and the file exists.
   - outputs:
     - Confirms the final contract fields and booleans before continuing the workflow.
   - on_error:
     - Re-run the script or correct the CDP URL if the file is missing or malformed.

---

## DATA FLOW

`{{TWITTER_CDP_URL}}` -> `test_cdp_connection.py` -> CDP browser handshake -> `Page.navigate` to `https://x.com/home` -> DOM/title snapshot checks -> `connection_test.json`

---

## OUTPUT FILE FORMATS

**File**: `runs/iteration-30/manish/execution/step-1/connection_test.json`

```json
{
  "connected": true,
  "twitter_visible": true,
  "cdp_url": "string",
  "error": null
}
```

- `connected`: boolean
- `twitter_visible`: boolean
- `cdp_url`: string
- `error`: string or null

---

## FAILURES TO AVOID

- Do not pass `host.docker.internal` directly to Chrome CDP. The browser rejects the host header unless the endpoint is already resolved to an IP or localhost.
- Do not treat a successful socket connection as enough. The step must also confirm the X/Twitter interface is visible after navigating to `https://x.com/home`.
- Do not continue the workflow if `connection_test.json` has `connected=false` or `twitter_visible=false`.
- Do not use a different browser session identifier with `agent-browser`; the workflow depends on the existing live CDP endpoint.

---

## SUCCESS PATTERN OBSERVED

- The script resolved the CDP endpoint, connected to the browser target, enabled `Page`, `Runtime`, and `Network`, applied a realistic Mac Chrome 131 user agent override, navigated to `https://x.com/home`, and then checked `document.title`, `location.href`, `document.documentElement.outerHTML`, and `document.body.innerText` until the Twitter/X UI was confirmed.
- The run completed with:

```json
{
  "connected": true,
  "twitter_visible": true,
  "cdp_url": "http://0.250.250.254:9222",
  "error": null
}
```

---

## SCRIPT

`runs/iteration-30/manish/execution/step-1/test_cdp_connection.py`

```python
import base64
import hashlib
import json
import os
import socket
import struct
import sys
import time
from pathlib import Path
from urllib.parse import urlparse
from urllib.request import Request, urlopen

WORKFLOW_ROOT = Path('runs/iteration-30/manish/execution')
STEP_DIR = WORKFLOW_ROOT / 'step-1'
OUTPUT_PATH = STEP_DIR / 'connection_test.json'
CDP_URL = 'http://0.250.250.254:9222'
TARGET_URL = 'https://x.com/home'
UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'
UA_META = {
    'brands': [
        {'brand': 'Google Chrome', 'version': '131'},
        {'brand': 'Chromium', 'version': '131'},
        {'brand': 'Not_A Brand', 'version': '24'},
    ],
    'fullVersionList': [
        {'brand': 'Google Chrome', 'version': '131.0.6778.86'},
        {'brand': 'Chromium', 'version': '131.0.6778.86'},
        {'brand': 'Not_A Brand', 'version': '24.0.0.0'},
    ],
    'fullVersion': '131.0.6778.86',
    'platform': 'macOS',
    'platformVersion': '10.15.7',
    'architecture': 'x86',
    'model': '',
    'mobile': False,
}


def ensure_step_dir() -> None:
    STEP_DIR.mkdir(parents=True, exist_ok=True)


def write_result(connected: bool, twitter_visible: bool, cdp_url: str, error: str | None = None) -> None:
    ensure_step_dir()
    payload = {
        'connected': connected,
        'twitter_visible': twitter_visible,
        'cdp_url': cdp_url,
        'error': error,
    }
    OUTPUT_PATH.write_text(json.dumps(payload, indent=2), encoding='utf-8')


def http_get_json(url: str):
    req = Request(url, headers={'User-Agent': UA, 'Accept-Language': 'en-US,en;q=0.9'})
    with urlopen(req, timeout=20) as resp:
        return json.loads(resp.read().decode('utf-8'))


class CDPWebSocket:
    def __init__(self, ws_url: str):
        self.ws_url = ws_url
        self.sock = None
        self.next_id = 1
        self.recv_buffer = bytearray()

    def connect(self):
        parsed = urlparse(self.ws_url)
        host = parsed.hostname
        if host is None:
            raise RuntimeError('missing websocket host')
        port = parsed.port or (443 if parsed.scheme == 'wss' else 80)
        raw = socket.create_connection((host, port), timeout=20)
        if parsed.scheme == 'wss':
            import ssl
            ctx = ssl.create_default_context()
            self.sock = ctx.wrap_socket(raw, server_hostname=host)
        else:
            self.sock = raw
        key = base64.b64encode(os.urandom(16)).decode('ascii')
        path = parsed.path or '/'
        if parsed.query:
            path += '?' + parsed.query
        req = (
            f'GET {path} HTTP/1.1\r\n'
            f'Host: {host}:{port}\r\n'
            'Upgrade: websocket\r\n'
            'Connection: Upgrade\r\n'
            f'Sec-WebSocket-Key: {key}\r\n'
            'Sec-WebSocket-Version: 13\r\n\r\n'
        )
        self.sock.sendall(req.encode('ascii'))
        response = self._read_http_headers()
        if '101' not in response.split('\r\n', 1)[0]:
            raise RuntimeError(f'bad websocket handshake: {response.splitlines()[0] if response else "empty response"}')
        expected = base64.b64encode(hashlib.sha1((key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode('ascii')).digest()).decode('ascii')
        if f'Sec-WebSocket-Accept: {expected}' not in response:
            raise RuntimeError('websocket accept header mismatch')

    def _read_http_headers(self) -> str:
        data = b''
        while b'\r\n\r\n' not in data:
            chunk = self.sock.recv(4096)
            if not chunk:
                break
            data += chunk
        return data.decode('latin1')

    def _read_exact(self, n: int) -> bytes:
        while len(self.recv_buffer) < n:
            chunk = self.sock.recv(65536)
            if not chunk:
                raise RuntimeError('socket closed while reading frame')
            self.recv_buffer.extend(chunk)
        out = bytes(self.recv_buffer[:n])
        del self.recv_buffer[:n]
        return out

    def _read_frame(self):
        header = self._read_exact(2)
        b1, b2 = header[0], header[1]
        opcode = b1 & 0x0F
        masked = (b2 & 0x80) != 0
        length = b2 & 0x7F
        if length == 126:
            length = struct.unpack('!H', self._read_exact(2))[0]
        elif length == 127:
            length = struct.unpack('!Q', self._read_exact(8))[0]
        mask = self._read_exact(4) if masked else b''
        payload = bytearray(self._read_exact(length))
        if masked:
            for i in range(length):
                payload[i] ^= mask[i % 4]
        return opcode, bytes(payload)

    def _send_frame(self, payload: bytes):
        if self.sock is None:
            raise RuntimeError('socket not connected')
        header = bytearray([0x81])
        length = len(payload)
        if length < 126:
            header.append(0x80 | length)
        elif length < 65536:
            header.append(0x80 | 126)
            header.extend(struct.pack('!H', length))
        else:
            header.append(0x80 | 127)
            header.extend(struct.pack('!Q', length))
        mask = os.urandom(4)
        header.extend(mask)
        masked_payload = bytearray(payload)
        for i in range(length):
            masked_payload[i] ^= mask[i % 4]
        self.sock.sendall(bytes(header) + bytes(masked_payload))

    def send(self, method: str, params: dict | None = None):
        message_id = self.next_id
        self.next_id += 1
        payload = json.dumps({'id': message_id, 'method': method, 'params': params or {}}).encode('utf-8')
        self._send_frame(payload)
        while True:
            opcode, body = self._read_frame()
            if opcode == 8:
                raise RuntimeError('websocket closed by peer')
            if opcode != 1:
                continue
            msg = json.loads(body.decode('utf-8'))
            if msg.get('id') == message_id:
                if 'error' in msg:
                    raise RuntimeError(json.dumps(msg['error']))
                return msg.get('result', {})

    def close(self):
        try:
            if self.sock is not None:
                self.sock.close()
        finally:
            self.sock = None


def eval_expr(cdp: CDPWebSocket, expression: str):
    result = cdp.send('Runtime.evaluate', {
        'expression': expression,
        'returnByValue': True,
        'awaitPromise': True,
    })
    return result.get('result', {}).get('value')


def select_target(cdp_root: str) -> tuple[str, str]:
    version = http_get_json(cdp_root.rstrip('/') + '/json/version')
    root_ws = version.get('webSocketDebuggerUrl')
    if not root_ws:
        raise RuntimeError('missing browser websocket debugger URL')

    pages = http_get_json(cdp_root.rstrip('/') + '/json')
    page = None
    for candidate in pages:
        if candidate.get('type') == 'page' and candidate.get('url', '').startswith('https://x.com'):
            page = candidate
            break
    if page is None:
        for candidate in pages:
            if candidate.get('type') == 'page':
                page = candidate
                break
    if page and page.get('webSocketDebuggerUrl'):
        return page['webSocketDebuggerUrl'], page.get('url', '')

    browser = CDPWebSocket(root_ws)
    browser.connect()
    try:
        created = browser.send('Target.createTarget', {'url': 'about:blank'})
        target_id = created.get('targetId')
        if not target_id:
            raise RuntimeError('failed to create target')
        pages = http_get_json(cdp_root.rstrip('/') + '/json')
        for candidate in pages:
            if candidate.get('id') == target_id and candidate.get('webSocketDebuggerUrl'):
                return candidate['webSocketDebuggerUrl'], candidate.get('url', '')
        raise RuntimeError('created target missing websocket URL')
    finally:
        browser.close()


def main():
    ensure_step_dir()
    connected = False
    twitter_visible = False
    error = None
    try:
        ws_url, initial_url = select_target(CDP_URL)
        cdp = CDPWebSocket(ws_url)
        cdp.connect()
        connected = True
        cdp.send('Page.enable')
        cdp.send('Runtime.enable')
        cdp.send('Network.enable')
        cdp.send('Network.setUserAgentOverride', {
            'userAgent': UA,
            'acceptLanguage': 'en-US,en;q=0.9',
            'platform': 'MacIntel',
            'userAgentMetadata': UA_META,
        })
        cdp.send('Page.navigate', {'url': TARGET_URL})
        deadline = time.time() + 25
        checks = []
        while time.time() < deadline:
            time.sleep(2)
            title = eval_expr(cdp, 'document.title') or ''
            href = eval_expr(cdp, 'location.href') or ''
            html = eval_expr(cdp, 'document.documentElement.outerHTML.slice(0, 5000)') or ''
            text = eval_expr(cdp, 'document.body ? document.body.innerText.slice(0, 3000) : ""') or ''
            combined = ' '.join([title, href, html, text]).lower()
            checks.append({'title': title, 'href': href})
            if 'x.com' in href and any(token in combined for token in ['home', 'post', 'tweet', 'what is happening', 'timeline', 'for you', 'following', 'x']) and 'something went wrong' not in combined:
                twitter_visible = True
                break
        cdp.close()
        if not twitter_visible:
            error = f'Twitter UI not confirmed after navigation; checks={checks[-3:]}'
    except Exception as exc:
        error = str(exc)
    write_result(connected, twitter_visible, CDP_URL, error)
    data = json.loads(OUTPUT_PATH.read_text(encoding='utf-8'))
    if not isinstance(data.get('connected'), bool):
        print('FAIL: $.connected is not boolean')
        sys.exit(1)
    if not isinstance(data.get('twitter_visible'), bool):
        print('FAIL: $.twitter_visible is not boolean')
        sys.exit(1)
    if not isinstance(data.get('cdp_url'), str) or len(data['cdp_url']) < 5:
        print('FAIL: $.cdp_url is invalid')
        sys.exit(1)
    if not data['connected']:
        print(f"FAIL: connected=false error={data.get('error')}")
        sys.exit(1)
    if not data['twitter_visible']:
        print(f"FAIL: twitter_visible=false error={data.get('error')}")
        sys.exit(1)
    print('PASS: CDP connected and Twitter visible')


if __name__ == '__main__':
    main()
```

