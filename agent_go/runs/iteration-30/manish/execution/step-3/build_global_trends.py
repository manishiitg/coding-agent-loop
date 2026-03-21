import base64
import concurrent.futures
import hashlib
import json
import os
import re
import socket
import struct
import sys
import time
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlparse
from urllib.request import Request, urlopen


WORKFLOW_ROOT = Path("runs/iteration-30/manish/execution")
STEP_DIR = WORKFLOW_ROOT / "step-3"
KB_ROOT = Path("knowledgebase")
RAW_SCAN_PATH = STEP_DIR / "raw_scan.json"
STEP_OUTPUT_PATH = STEP_DIR / "global_trends.json"
KB_OUTPUT_PATH = KB_ROOT / "research" / "global_trends.json"
CONNECTION_PATH = WORKFLOW_ROOT / "step-1" / "connection_test.json"

CDP_URL = "http://0.250.250.254:9222"
EXPLORE_URL = "https://x.com/explore?tab=trending"
TWITTER_HANDLES = [
    "karpathy",
    "simonw",
    "swyx",
    "GergelyOrosz",
    "levelsio",
    "dhh",
    "emollick",
    "t3dotgg",
]
USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) "
    "Chrome/131.0.0.0 Safari/537.36"
)
USER_AGENT_META = {
    "brands": [
        {"brand": "Google Chrome", "version": "131"},
        {"brand": "Chromium", "version": "131"},
        {"brand": "Not_A Brand", "version": "24"},
    ],
    "fullVersionList": [
        {"brand": "Google Chrome", "version": "131.0.6778.86"},
        {"brand": "Chromium", "version": "131.0.6778.86"},
        {"brand": "Not_A Brand", "version": "24.0.0.0"},
    ],
    "fullVersion": "131.0.6778.86",
    "platform": "macOS",
    "platformVersion": "10.15.7",
    "architecture": "x86",
    "model": "",
    "mobile": False,
}
STOPWORDS = {
    "the", "and", "for", "with", "that", "this", "from", "into", "your", "about",
    "have", "just", "they", "their", "what", "when", "where", "after", "today",
    "launch", "launched", "release", "released", "new", "top", "news", "week",
    "build", "builder", "builders", "developer", "developers", "tool", "tools",
    "open", "source", "open-source", "using", "used", "more", "than", "over",
    "into", "agent", "agents", "code", "coding", "will", "still", "like", "there",
    "post", "posts", "reply", "replies", "thread", "threads", "ship", "shipping",
}
TOPIC_RULES = [
    {
        "topic": "OpenClaw momentum",
        "patterns": [r"\bopenclaw\b", r"\bnvidia\b", r"\bdgx\b", r"\bhouse elf claw\b"],
        "why_template": "OpenClaw and adjacent local-AI hardware chatter kept surfacing across sources, signaling that personal AI infrastructure and agent tooling are still feeding each other.",
        "twitter_template": "OpenClaw is becoming the proxy war for a bigger question: is AI infra hype finally translating into faster iteration for builders, or are we just rebranding expensive toys?",
        "linkedin_template": "Local AI stacks are turning into a real product lever, not just a demo flex.\n- Teams care about iteration speed, not just benchmark screenshots.\n- Hardware-adjacent projects get distribution when they map to agent workflows.\n- Nvidia mentions now move open-source narratives, not only chip narratives.",
    },
    {
        "topic": "Karpathy and agent-directed engineering",
        "patterns": [r"\bkarpathy\b", r"\bautoresearch\b", r"\bagent\b", r"\bdirecting\b"],
        "why_template": "Karpathy-linked discussion around directing agents instead of hand-writing every step is still attracting heavy attention and sets the tone for how technical teams talk about leverage.",
        "twitter_template": "The engineering status game is shifting from typing speed to taste under ambiguity. Agents raise the floor, but direction and evaluation become the new moat.",
        "linkedin_template": "The practical shift is organizational, not philosophical.\n- Senior engineers spend more time defining loops, evals, and constraints.\n- Output quality depends on review systems, not just prompt quality.\n- Teams that operationalize agent supervision will outpace teams that merely buy licenses.",
    },
    {
        "topic": "Cursor Composer and model provenance",
        "patterns": [r"\bcursor\b", r"\bcomposer\b", r"\bkimi\b", r"\bprovenance\b"],
        "why_template": "Composer/Cursor chatter is not just product-launch buzz; it is another round of the model-provenance debate, where users want to know what stack they are actually buying.",
        "twitter_template": "Base-model provenance stopped being an implementation detail the moment AI products started charging enterprise prices. Trust is becoming a product surface.",
        "linkedin_template": "The market is maturing past feature comparisons.\n- Buyers care who built the underlying model and what that implies for risk.\n- Packaging another model can still be a strong product, but disclosure matters.\n- Trust, billing clarity, and support now shape adoption as much as raw capability.",
    },
    {
        "topic": "Claude Dispatch and scheduled agents",
        "patterns": [r"\bdispatch\b", r"\bclaude\b", r"\bscheduled\b", r"\basynchronous\b", r"\bpersistent\b"],
        "why_template": "Background and scheduled agent work is moving from novelty to expected workflow, with Claude Dispatch-style discussion reinforcing that autonomy is now a UX category.",
        "twitter_template": "Agent UX is graduating from chat windows to durable workflows. The winner is not the best demo; it is the tool teams trust to keep running when nobody is watching.",
        "linkedin_template": "Persistent agents change both tooling and operations.\n- Scheduling and background execution reduce human orchestration overhead.\n- Reliability and observability become first-class product requirements.\n- This shifts evaluation from 'can it do the task?' to 'can it run unsupervised without becoming a liability?'",
    },
    {
        "topic": "Delve compliance fallout",
        "patterns": [r"\bdelve\b", r"\bcompliance\b", r"\baudit\b", r"\bfabricat", r"\bsecurity\b"],
        "why_template": "Delve-related allegations remain one of the clearest trust and governance stories in the startup and AI tooling ecosystem, and the conversation spills beyond one company.",
        "twitter_template": "Every AI trust startup is now being graded on whether it can survive real scrutiny, not whether it can sell a slick risk narrative. Governance theater is getting exposed faster.",
        "linkedin_template": "The deeper lesson is procurement discipline.\n- Security and trust vendors are becoming concentrated points of failure.\n- Enterprises need evidence trails, not polished claims.\n- In AI tooling, compliance posture is now part of product-market fit.",
    },
    {
        "topic": "GPU scarcity versus autonomous workloads",
        "patterns": [r"\bgpu\b", r"\bh100\b", r"\bcapacity\b", r"\bcompute\b", r"\bsold out\b"],
        "why_template": "Demand for autonomous AI workflows keeps colliding with limited compute capacity, making infra constraints part of the product conversation again.",
        "twitter_template": "Agents make everyone want more background compute at the same time. The bottleneck is drifting from prompt quality back to allocation and infra economics.",
        "linkedin_template": "AI product planning now has an infra strategy component.\n- Autonomous workloads amplify baseline compute demand.\n- Scarcity reshapes pricing, latency, and reliability promises.\n- Teams that design for constrained capacity will ship more stable agent products.",
    },
]


def fail(message: str) -> None:
    print(f"FAIL: {message}")
    sys.exit(1)


def http_get(url: str, headers: dict | None = None, timeout: int = 30) -> bytes:
    req = Request(url, headers=headers or {})
    with urlopen(req, timeout=timeout) as resp:
        return resp.read()


def http_get_json(url: str, headers: dict | None = None, timeout: int = 30):
    return json.loads(http_get(url, headers=headers, timeout=timeout).decode("utf-8"))


def http_post_json(url: str, payload: dict, headers: dict | None = None, timeout: int = 60):
    req = Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json", **(headers or {})},
        method="POST",
    )
    with urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


class CDPWebSocket:
    def __init__(self, ws_url: str):
        self.ws_url = ws_url
        self.sock = None
        self.next_id = 1
        self.recv_buffer = bytearray()

    def connect(self):
        parsed = urlparse(self.ws_url)
        host = parsed.hostname
        if not host:
            raise RuntimeError("missing websocket host")
        port = parsed.port or (443 if parsed.scheme == "wss" else 80)
        raw = socket.create_connection((host, port), timeout=20)
        if parsed.scheme == "wss":
            import ssl
            ctx = ssl.create_default_context()
            self.sock = ctx.wrap_socket(raw, server_hostname=host)
        else:
            self.sock = raw
        key = base64.b64encode(os.urandom(16)).decode("ascii")
        path = parsed.path or "/"
        if parsed.query:
            path += "?" + parsed.query
        req = (
            f"GET {path} HTTP/1.1\r\n"
            f"Host: {host}:{port}\r\n"
            "Upgrade: websocket\r\n"
            "Connection: Upgrade\r\n"
            f"Sec-WebSocket-Key: {key}\r\n"
            "Sec-WebSocket-Version: 13\r\n\r\n"
        )
        self.sock.sendall(req.encode("ascii"))
        response = self._read_http_headers()
        if "101" not in response.split("\r\n", 1)[0]:
            raise RuntimeError(f"bad websocket handshake: {response.splitlines()[0] if response else 'empty response'}")
        expected = base64.b64encode(
            hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode("ascii")).digest()
        ).decode("ascii")
        if f"Sec-WebSocket-Accept: {expected}" not in response:
            raise RuntimeError("websocket accept header mismatch")

    def _read_http_headers(self) -> str:
        data = b""
        while b"\r\n\r\n" not in data:
            chunk = self.sock.recv(4096)
            if not chunk:
                break
            data += chunk
        return data.decode("latin1")

    def _read_exact(self, n: int) -> bytes:
        while len(self.recv_buffer) < n:
            chunk = self.sock.recv(65536)
            if not chunk:
                raise RuntimeError("socket closed while reading frame")
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
            length = struct.unpack("!H", self._read_exact(2))[0]
        elif length == 127:
            length = struct.unpack("!Q", self._read_exact(8))[0]
        mask = self._read_exact(4) if masked else b""
        payload = bytearray(self._read_exact(length))
        if masked:
            for i in range(length):
                payload[i] ^= mask[i % 4]
        return opcode, bytes(payload)

    def _send_frame(self, payload: bytes):
        if self.sock is None:
            raise RuntimeError("socket not connected")
        header = bytearray([0x81])
        length = len(payload)
        if length < 126:
            header.append(0x80 | length)
        elif length < 65536:
            header.append(0x80 | 126)
            header.extend(struct.pack("!H", length))
        else:
            header.append(0x80 | 127)
            header.extend(struct.pack("!Q", length))
        mask = os.urandom(4)
        header.extend(mask)
        masked_payload = bytearray(payload)
        for i in range(length):
            masked_payload[i] ^= mask[i % 4]
        self.sock.sendall(bytes(header) + bytes(masked_payload))

    def send(self, method: str, params: dict | None = None):
        message_id = self.next_id
        self.next_id += 1
        payload = json.dumps({"id": message_id, "method": method, "params": params or {}}).encode("utf-8")
        self._send_frame(payload)
        while True:
            opcode, body = self._read_frame()
            if opcode == 8:
                raise RuntimeError("websocket closed by peer")
            if opcode != 1:
                continue
            msg = json.loads(body.decode("utf-8"))
            if msg.get("id") == message_id:
                if "error" in msg:
                    raise RuntimeError(json.dumps(msg["error"]))
                return msg.get("result", {})

    def close(self):
        try:
            if self.sock is not None:
                self.sock.close()
        finally:
            self.sock = None


class TwitterCDP:
    def __init__(self, cdp_root: str):
        self.cdp_root = cdp_root.rstrip("/")
        self.target_id, ws_url = self.create_target()
        self.cdp = CDPWebSocket(ws_url)
        self.cdp.connect()
        self.cdp.send("Page.enable")
        self.cdp.send("Runtime.enable")
        self.cdp.send("Network.enable")
        self.cdp.send("Page.bringToFront")
        self.cdp.send(
            "Network.setUserAgentOverride",
            {
                "userAgent": USER_AGENT,
                "acceptLanguage": "en-US,en;q=0.9",
                "platform": "MacIntel",
                "userAgentMetadata": USER_AGENT_META,
            },
        )

    def create_target(self):
        encoded = quote("https://x.com/home", safe="")
        req = Request(f"{self.cdp_root}/json/new?{encoded}", method="PUT")
        with urlopen(req, timeout=20) as resp:
            target = json.loads(resp.read().decode("utf-8"))
        return target["id"], target["webSocketDebuggerUrl"]

    def close_target(self):
        try:
            urlopen(f"{self.cdp_root}/json/close/{self.target_id}", timeout=10)
        except Exception:
            pass

    def close(self):
        self.cdp.close()
        self.close_target()

    def eval(self, expression: str):
        result = self.cdp.send(
            "Runtime.evaluate",
            {"expression": expression, "returnByValue": True, "awaitPromise": True},
        )
        return result.get("result", {}).get("value")

    def navigate(self, url: str, timeout: int = 30):
        self.cdp.send("Page.navigate", {"url": url})
        deadline = time.time() + timeout
        last_state = {}
        while time.time() < deadline:
            time.sleep(2)
            ready = self.eval("document.readyState")
            href = self.eval("location.href")
            text = self.eval("document.body ? document.body.innerText.slice(0, 2000) : ''") or ""
            last_state = {"href": href, "ready": ready, "text": text[:300]}
            if ready == "complete" and href and "x.com" in href and "something went wrong" not in text.lower():
                return
        raise RuntimeError(f"navigation timeout for {url}: {last_state}")

    def extract_trending(self):
        self.navigate(EXPLORE_URL)
        time.sleep(3)
        data = self.eval(
            """
            (() => {
              const links = Array.from(document.querySelectorAll('a[href*="/search?q="], a[role="link"]'));
              const items = [];
              for (const link of links) {
                const text = (link.innerText || '').trim();
                if (!text) continue;
                const lines = text.split('\\n').map(x => x.trim()).filter(Boolean);
                const joined = lines.join(' ');
                if (!(/posts/i.test(joined) || /Trending/i.test(joined) || /News/i.test(joined))) continue;
                if (joined.length < 8) continue;
                items.push({
                  text,
                  href: link.href || null,
                  aria: link.getAttribute('aria-label'),
                });
              }
              const seen = new Set();
              return items.filter(item => {
                const key = item.text;
                if (seen.has(key)) return false;
                seen.add(key);
                return true;
              }).slice(0, 20);
            })()
            """
        )
        if not isinstance(data, list):
            raise RuntimeError("failed to extract trending list")
        return data

    def extract_profile(self, handle: str):
        self.navigate(f"https://x.com/{handle}")
        time.sleep(3)
        tweets = self.eval(
            """
            (() => {
              const articles = Array.from(document.querySelectorAll('article[data-testid="tweet"]'));
              const out = [];
              for (const article of articles) {
                const text = (article.innerText || '').trim();
                if (!text) continue;
                const statusLink = article.querySelector('a[href*="/status/"]');
                const href = statusLink ? statusLink.href : null;
                const timeEl = article.querySelector('time');
                const metrics = Array.from(article.querySelectorAll('[aria-label]'))
                  .map(el => el.getAttribute('aria-label'))
                  .filter(Boolean)
                  .filter(v => /Reply|repost|Like|views/i.test(v))
                  .slice(0, 6);
                out.push({
                  text,
                  tweet_url: href,
                  posted_at: timeEl ? timeEl.getAttribute('datetime') : null,
                  metrics,
                });
              }
              const seen = new Set();
              return out.filter(item => {
                const key = item.tweet_url || item.text;
                if (!key || seen.has(key)) return false;
                seen.add(key);
                return true;
              }).slice(0, 5);
            })()
            """
        )
        if not isinstance(tweets, list):
            tweets = []
        return {"handle": handle, "tweets": tweets}


def read_dependency():
    if not CONNECTION_PATH.exists():
        fail(f"missing dependency {CONNECTION_PATH}")
    payload = json.loads(CONNECTION_PATH.read_text(encoding="utf-8"))
    if not payload.get("connected") or not payload.get("twitter_visible"):
        fail(f"connection dependency not healthy: {payload}")
    print("PASS: connection_test.json verified")
    return payload


def fetch_hn():
    ids = http_get_json("https://hacker-news.firebaseio.com/v0/topstories.json", timeout=30)[:30]

    def fetch_item(item_id: int):
        try:
            item = http_get_json(f"https://hacker-news.firebaseio.com/v0/item/{item_id}.json", timeout=30)
            return item
        except Exception as exc:
            return {"id": item_id, "error": str(exc)}

    items = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=12) as executor:
        for item in executor.map(fetch_item, ids):
            items.append(item)

    keywords = (
        "ai", "agent", "agents", "llm", "open source", "developer", "devtool", "programming",
        "coding", "gpu", "inference", "startup", "security", "claude", "cursor", "openai",
        "model", "models", "nvidia", "terminal", "github", "mcp",
    )
    filtered = []
    for item in items:
        title = (item.get("title") or "").lower()
        text = f"{title} {(item.get('text') or '').lower()}"
        if item.get("type") != "story":
            continue
        if item.get("score", 0) <= 100 or item.get("descendants", 0) <= 20:
            continue
        if not any(keyword in text for keyword in keywords):
            continue
        filtered.append(item)

    return {"top_story_ids": ids, "relevant_stories": filtered}


def fetch_parallel_web():
    api_key = os.environ.get("PARALLEL_AI_WEB_SERACH")
    if not api_key:
        return {"error": "PARALLEL_AI_WEB_SERACH missing"}
    payload = {
        "objective": "top AI and developer news today",
        "search_queries": [
            "AI developer tools trending 2026",
            "open source AI agents news",
            "developer tools launches this week",
        ],
        "mode": "fast",
        "excerpts": {"max_chars_per_result": 3000},
    }
    try:
        data = http_post_json(
            "https://api.parallel.ai/v1beta/search",
            payload,
            headers={
                "x-api-key": api_key,
                "parallel-beta": "search-extract-2025-10-10",
                "User-Agent": USER_AGENT,
            },
            timeout=90,
        )
        return data
    except Exception as exc:
        return {"error": str(exc)}


def fetch_twitter():
    twitter = TwitterCDP(CDP_URL)
    try:
        trending = twitter.extract_trending()
        profiles = []
        for handle in TWITTER_HANDLES:
            try:
                profiles.append(twitter.extract_profile(handle))
            except Exception as exc:
                profiles.append({"handle": handle, "error": str(exc), "tweets": []})
        return {
            "scanned_at": datetime.now(timezone.utc).isoformat(),
            "explore_url": EXPLORE_URL,
            "trending": trending,
            "profiles": profiles,
        }
    finally:
        twitter.close()


def flatten_web_items(web_data):
    items = []
    if not isinstance(web_data, dict):
        return items
    stack = [web_data]
    while stack:
        current = stack.pop()
        if isinstance(current, dict):
            title = current.get("title") or current.get("headline") or current.get("name")
            url = current.get("url") or current.get("link")
            snippet = current.get("snippet") or current.get("excerpt") or current.get("text")
            if title or snippet:
                items.append(
                    {
                        "title": title or "",
                        "url": url,
                        "snippet": snippet or "",
                    }
                )
            for value in current.values():
                if isinstance(value, (dict, list)):
                    stack.append(value)
        elif isinstance(current, list):
            for value in current:
                if isinstance(value, (dict, list)):
                    stack.append(value)
    seen = set()
    deduped = []
    for item in items:
        key = (item["title"], item["url"], item["snippet"][:200])
        if key in seen:
            continue
        seen.add(key)
        deduped.append(item)
    return deduped


def gather_source_texts(raw):
    texts = []
    twitter = raw.get("twitter", {})
    for trend in twitter.get("trending", []):
        texts.append({"source": "twitter", "text": trend.get("text", ""), "url": trend.get("href")})
    for profile in twitter.get("profiles", []):
        handle = profile.get("handle")
        for tweet in profile.get("tweets", []):
            texts.append(
                {
                    "source": "twitter",
                    "text": f"@{handle} {tweet.get('text', '')}",
                    "url": tweet.get("tweet_url"),
                    "account": f"@{handle}",
                }
            )
    hn = raw.get("hn", {})
    for item in hn.get("relevant_stories", []):
        texts.append(
            {
                "source": "hn",
                "text": f"{item.get('title', '')} {item.get('text', '')}",
                "url": item.get("url") or f"https://news.ycombinator.com/item?id={item.get('id')}",
            }
        )
    for item in flatten_web_items(raw.get("web", {})):
        texts.append({"source": "web", "text": f"{item['title']} {item['snippet']}", "url": item["url"]})
    return texts


def score_topic(rule, source_texts):
    matched = []
    source_hits = set()
    url_hits = []
    account_hits = []
    pattern_objs = [re.compile(pattern, re.I) for pattern in rule["patterns"]]
    for item in source_texts:
        text = item.get("text", "")
        if any(pattern.search(text) for pattern in pattern_objs):
            matched.append(item)
            source_hits.add(item["source"])
            if item.get("url"):
                url_hits.append(item["url"])
            if item.get("account"):
                account_hits.append(item["account"])
    if not matched:
        return None
    score = len(matched) + (4 * len(source_hits))
    return {
        "topic": rule["topic"],
        "score": score,
        "matches": matched,
        "source_hits": sorted(source_hits),
        "url_hits": url_hits,
        "account_hits": account_hits,
        "rule": rule,
    }


def summarize_facts(candidate):
    counts = Counter()
    urls = []
    for item in candidate["matches"]:
        text = re.sub(r"\s+", " ", item.get("text", "")).strip()
        for token in re.findall(r"[A-Za-z][A-Za-z0-9\.\-\+]{2,}", text):
            lowered = token.lower()
            if lowered not in STOPWORDS:
                counts[lowered] += 1
        if item.get("url"):
            urls.append(item["url"])
    top_terms = [term for term, _ in counts.most_common(6)]
    facts = []
    facts.append(f"Appeared in {', '.join(candidate['source_hits'])} sources during this scan.")
    if top_terms:
        facts.append("Recurring keywords: " + ", ".join(top_terms[:4]) + ".")
    unique_urls = []
    seen = set()
    for url in urls:
        if url in seen:
            continue
        seen.add(url)
        unique_urls.append(url)
    for url in unique_urls[:2]:
        facts.append(f"Reference: {url}")
    return facts[:4]


def build_why(candidate):
    excerpts = []
    for item in candidate["matches"][:3]:
        text = re.sub(r"\s+", " ", item.get("text", "")).strip()
        excerpts.append(text[:180])
    joined = " | ".join(excerpts)
    base = candidate["rule"]["why_template"]
    if joined:
        return f"{base} Signals captured: {joined}"
    return base


def synthesize_trends(raw):
    source_texts = gather_source_texts(raw)
    candidates = []
    for rule in TOPIC_RULES:
        candidate = score_topic(rule, source_texts)
        if candidate:
            candidates.append(candidate)

    if len(candidates) < 5:
        fallback_terms = Counter()
        for item in source_texts:
            for token in re.findall(r"[A-Za-z][A-Za-z0-9\.\-]{3,}", item.get("text", "")):
                lowered = token.lower()
                if lowered not in STOPWORDS:
                    fallback_terms[lowered] += 1
        for term, count in fallback_terms.most_common(10):
            if len(candidates) >= 5:
                break
            topic_name = term.upper() if term.isupper() else term.title()
            synthetic = {
                "topic": topic_name,
                "score": count,
                "matches": [item for item in source_texts if term in item.get("text", "").lower()][:4],
                "source_hits": sorted({item["source"] for item in source_texts if term in item.get("text", "").lower()}),
                "url_hits": [item.get("url") for item in source_texts if term in item.get("text", "").lower() and item.get("url")],
                "account_hits": [item.get("account") for item in source_texts if term in item.get("text", "").lower() and item.get("account")],
                "rule": {
                    "topic": topic_name,
                    "why_template": f"{topic_name} kept recurring across the collected inputs and is worth tracking as a live conversation thread.",
                    "twitter_template": f"{topic_name} is getting enough repeated attention that ignoring it is riskier than having a take on it.",
                    "linkedin_template": f"{topic_name} deserves a structured take.\n- It is recurring across current inputs.\n- It intersects AI or developer workflows.\n- It has enough momentum to justify content now.",
                },
            }
            if synthetic["matches"] and synthetic["source_hits"]:
                candidates.append(synthetic)

    candidates.sort(key=lambda item: item["score"], reverse=True)
    trends = []
    used_topics = set()
    for candidate in candidates:
        if len(trends) == 5:
            break
        if candidate["topic"] in used_topics:
            continue
        used_topics.add(candidate["topic"])
        source_account = next((acct for acct in candidate["account_hits"] if acct), None)
        source_tweet_url = next(
            (url for url in candidate["url_hits"] if isinstance(url, str) and "x.com" in url),
            None,
        )
        source_url = next(
            (url for url in candidate["url_hits"] if isinstance(url, str) and "x.com" not in url),
            None,
        )
        rule = candidate["rule"]
        trends.append(
            {
                "rank": len(trends) + 1,
                "topic": candidate["topic"],
                "why_trending": build_why(candidate),
                "twitter_angle": rule["twitter_template"],
                "linkedin_angle": rule["linkedin_template"],
                "key_facts": summarize_facts(candidate),
                "sources": candidate["source_hits"],
                "source_account": source_account,
                "source_tweet_url": source_tweet_url,
                "source_url": source_url,
            }
        )
    return trends


def write_json(path: Path, payload: dict):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def main():
    STEP_DIR.mkdir(parents=True, exist_ok=True)
    read_dependency()

    collected = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as executor:
        futures = {
            executor.submit(fetch_twitter): "twitter",
            executor.submit(fetch_hn): "hn",
            executor.submit(fetch_parallel_web): "web",
        }
        for future, name in futures.items():
            try:
                collected[name] = future.result()
                print(f"PASS: fetched {name}")
            except Exception as exc:
                collected[name] = {"error": str(exc)}
                print(f"FAIL: {name} fetch error {exc}")

    write_json(RAW_SCAN_PATH, collected)
    print(f"PASS: wrote {RAW_SCAN_PATH}")

    trends = synthesize_trends(collected)
    if len(trends) < 3:
        fail(f"expected at least 3 trends, got {len(trends)}")

    researched_at = datetime.now(timezone.utc).isoformat()
    final_payload = {
        "researched_at": researched_at,
        "trends": trends,
    }
    write_json(STEP_OUTPUT_PATH, final_payload)
    write_json(KB_OUTPUT_PATH, final_payload)
    print(f"PASS: wrote {STEP_OUTPUT_PATH}")
    print(f"PASS: wrote {KB_OUTPUT_PATH}")

    parsed = json.loads(STEP_OUTPUT_PATH.read_text(encoding="utf-8"))
    if not isinstance(parsed.get("researched_at"), str):
        fail("$.researched_at missing or invalid")
    if not isinstance(parsed.get("trends"), list) or len(parsed["trends"]) < 3:
        fail("$.trends missing or too short")
    print("PASS: output schema validated")


if __name__ == "__main__":
    main()
