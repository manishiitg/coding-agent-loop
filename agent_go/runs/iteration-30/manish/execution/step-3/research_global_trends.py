#!/usr/bin/env python3
import concurrent.futures
import datetime as dt
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


ROOT = Path("runs/iteration-30/manish/execution")
STEP_DIR = ROOT / "step-3"
STEP_OUTPUT = STEP_DIR / "global_trends.json"
RAW_SCAN_OUTPUT = STEP_DIR / "raw_scan.json"
KB_OUTPUT = Path("knowledgebase/research/global_trends.json")
CONNECTION_FILE = ROOT / "step-1/connection_test.json"
CDP_URL = "http://0.250.250.254:9222"
HANDLES = [
    "karpathy",
    "simonw",
    "swyx",
    "GergelyOrosz",
    "levelsio",
    "dhh",
    "emollick",
    "t3dotgg",
]
SEARCH_QUERIES = [
    "AI developer tools trending today March 2026",
    "open source AI agents news this week March 2026",
    "developer tools launches this week March 2026",
]
KEYWORDS = [
    "ai",
    "llm",
    "agent",
    "agents",
    "model",
    "models",
    "developer",
    "devtools",
    "coding",
    "code",
    "api",
    "open source",
    "opensource",
    "claude",
    "gpt",
    "openai",
    "anthropic",
    "google",
    "gemini",
    "cursor",
    "mcp",
]


def fail(message: str) -> None:
    print(f"FAIL: {message}")
    sys.exit(1)


def ensure_exists(path: Path) -> None:
    if not path.exists():
        fail(f"missing required file: {path}")


def load_json(path: Path):
    ensure_exists(path)
    return json.loads(path.read_text())


def strip_ansi(text: str) -> str:
    return re.sub(r"\x1b\[[0-9;]*m", "", text)


def extract_json_blob(text: str):
    cleaned = strip_ansi(text).strip()
    if not cleaned:
        return None
    for candidate in (cleaned, cleaned.splitlines()[-1].strip()):
        if not candidate:
            continue
        try:
            return json.loads(candidate)
        except json.JSONDecodeError:
            pass
    starts = [i for i, ch in enumerate(cleaned) if ch in "[{"]
    for start in starts:
        for end in range(len(cleaned), start, -1):
            snippet = cleaned[start:end].strip()
            try:
                return json.loads(snippet)
            except json.JSONDecodeError:
                continue
    return None


def run_agent_browser(*args: str) -> str:
    proc = subprocess.run(
        ["agent-browser", "--cdp", CDP_URL, *args],
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"agent-browser failed: {proc.stderr.strip() or proc.stdout.strip()}")
    return proc.stdout


def js_eval(url: str, script: str):
    run_agent_browser("open", url)
    time.sleep(3)
    raw = run_agent_browser("eval", script)
    return extract_json_blob(raw)


def normalize_text(value: str) -> str:
    return re.sub(r"\s+", " ", (value or "")).strip()


def is_relevant_text(text: str) -> bool:
    lowered = (text or "").lower()
    return any(keyword in lowered for keyword in KEYWORDS)


def collect_twitter():
    explore_primary = """
JSON.stringify(
  Array.from(document.querySelectorAll('[data-testid="trend"]')).slice(0,10).map(el => ({
    topic: el.querySelector('[dir="ltr"]')?.innerText || el.innerText.split('\\n')[0],
    posts: el.innerText.match(/([\\d.]+K?) posts/)?.[1] || null
  }))
)
"""
    explore_fallback = """
JSON.stringify(
  Array.from(document.querySelectorAll('div[aria-label] > div > div'))
    .filter(el => el.innerText && (el.innerText.includes('posts') || el.innerText.toLowerCase().includes('trending')))
    .slice(0,10)
    .map(el => ({topic: el.innerText.split('\\n')[0], posts: null}))
)
"""
    explore = js_eval("https://x.com/explore?tab=trending", explore_primary) or []
    if not explore:
        explore = js_eval("https://x.com/explore?tab=trending", explore_fallback) or []
    profiles = {}
    profile_js = """
JSON.stringify(
  Array.from(document.querySelectorAll('[data-testid="tweet"]'))
    .filter(t => !t.querySelector('[data-testid="socialContext"]'))
    .slice(0,5)
    .map(t => ({
      text: t.querySelector('[data-testid="tweetText"]')?.innerText || '',
      url: t.querySelector('a[href*="/status/"]')?.href || ''
    }))
)
"""
    for handle in HANDLES:
        posts = js_eval(f"https://x.com/{handle}", profile_js) or []
        profiles[handle] = [
            {
                "text": normalize_text(post.get("text", "")),
                "url": post.get("url") or None,
            }
            for post in posts
            if normalize_text(post.get("text", ""))
        ]
    return {
        "explore": {
            "trends": [
                {
                    "topic": normalize_text(item.get("topic", "")),
                    "posts": item.get("posts"),
                }
                for item in explore
                if normalize_text(item.get("topic", ""))
            ],
            "error": None if explore else "extraction_failed",
        },
        "profiles": profiles,
    }


def collect_techmeme():
    techmeme_js = """
JSON.stringify(
  Array.from(document.querySelectorAll('.ourh, .hl')).slice(0,15).map(el => ({
    text: el.innerText.trim().substring(0,150),
    href: el.href || el.querySelector('a')?.href || null
  }))
)
"""
    headlines = js_eval("https://www.techmeme.com/", techmeme_js) or []
    cleaned = []
    for item in headlines:
        text = normalize_text(item.get("text", ""))
        if text and is_relevant_text(text):
            cleaned.append({"text": text, "href": item.get("href") or None})
    return cleaned


def fetch_json_url(url: str):
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(req, timeout=30) as response:
        return json.loads(response.read().decode("utf-8"))


def collect_hn():
    story_ids = fetch_json_url("https://hacker-news.firebaseio.com/v0/topstories.json")[:30]

    def fetch_story(story_id: int):
        try:
            return fetch_json_url(f"https://hacker-news.firebaseio.com/v0/item/{story_id}.json")
        except Exception:
            return None

    with concurrent.futures.ThreadPoolExecutor(max_workers=10) as executor:
        stories = list(executor.map(fetch_story, story_ids))

    relevant = []
    for story in stories:
        if not story:
            continue
        title = story.get("title", "")
        url = story.get("url", "")
        if story.get("score", 0) > 100 and story.get("descendants", 0) > 20 and is_relevant_text(f"{title} {url}"):
            relevant.append(
                {
                    "id": story.get("id"),
                    "title": normalize_text(title),
                    "url": url or f"https://news.ycombinator.com/item?id={story.get('id')}",
                    "score": story.get("score", 0),
                    "comments": story.get("descendants", 0),
                }
            )
    return {"top_story_ids": story_ids, "relevant_stories": relevant}


def mcp_post(path: str, payload: dict):
    base = os.environ.get("MCP_API_URL")
    token = os.environ.get("MCP_API_TOKEN", "")
    if not base:
        raise RuntimeError("MCP_API_URL is not set")
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        base.rstrip("/") + path,
        data=data,
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=60) as response:
        body = json.loads(response.read().decode("utf-8"))
    if not body.get("success"):
        raise RuntimeError(body.get("error") or f"MCP request failed for {path}")
    return body.get("result")


def collect_web():
    def one_search(query: str):
        try:
            result = mcp_post("/tools/mcp/minimax/web_search", {"query": query})
            organic = result.get("organic", []) if isinstance(result, dict) else []
            return {
                "query": query,
                "organic": organic[:5],
                "related_searches": result.get("related_searches", []) if isinstance(result, dict) else [],
            }
        except Exception as exc:
            return {"query": query, "error": str(exc), "organic": []}

    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as executor:
        return list(executor.map(one_search, SEARCH_QUERIES))


def title_tokens(text: str):
    words = re.findall(r"[A-Za-z0-9][A-Za-z0-9+._-]*", text or "")
    stop = {
        "the",
        "and",
        "with",
        "from",
        "this",
        "that",
        "into",
        "over",
        "after",
        "today",
        "march",
        "week",
        "launches",
        "trending",
        "news",
        "open",
        "source",
        "build",
    }
    return [w for w in words if len(w) > 2 and w.lower() not in stop]


def choose_topic_label(*texts: str) -> str:
    freq = {}
    for text in texts:
        for token in title_tokens(text):
            freq[token] = freq.get(token, 0) + 1
    if not freq:
        return "AI developer tooling"
    top = sorted(freq.items(), key=lambda kv: (-kv[1], -len(kv[0]), kv[0].lower()))[:3]
    return " ".join(token for token, _ in top)


def first_sentence(text: str) -> str:
    text = normalize_text(text)
    if not text:
        return ""
    parts = re.split(r"(?<=[.!?])\s+", text)
    return parts[0][:220]


def build_candidates(raw_scan: dict):
    candidates = []

    for item in raw_scan["twitter"]["explore"]["trends"]:
        topic = item.get("topic", "")
        if is_relevant_text(topic):
            candidates.append(
                {
                    "topic_seed": topic,
                    "why": f"Twitter Explore is surfacing {topic} directly, indicating live attention from the broader X feed.",
                    "sources": {"twitter"},
                    "twitter_source": {"account": None, "tweet_url": None},
                    "source_url": None,
                    "facts": [f"Twitter Explore listed {topic}." + (f" The card showed about {item['posts']} posts." if item.get("posts") else "")],
                }
            )

    for handle, posts in raw_scan["twitter"]["profiles"].items():
        for post in posts:
            text = post.get("text", "")
            if is_relevant_text(text):
                snippet = first_sentence(text)
                candidates.append(
                    {
                        "topic_seed": snippet,
                        "why": f"Builder Twitter is actively discussing this through @{handle}, which makes it a high-signal operator conversation rather than a generic news spike.",
                        "sources": {"twitter"},
                        "twitter_source": {"account": f"@{handle}", "tweet_url": post.get("url")},
                        "source_url": post.get("url"),
                        "facts": [f"@{handle} posted: {snippet}"],
                    }
                )

    for item in raw_scan["techmeme"]:
        text = item.get("text", "")
        candidates.append(
            {
                "topic_seed": text,
                "why": "Techmeme is curating this into the main tech news cycle, which usually means broader industry awareness beyond a single platform.",
                "sources": {"techmeme"},
                "twitter_source": {"account": None, "tweet_url": None},
                "source_url": item.get("href"),
                "facts": [f"Techmeme headline: {text}"],
            }
        )

    for story in raw_scan["hn"]["relevant_stories"]:
        candidates.append(
            {
                "topic_seed": story["title"],
                "why": "Hacker News traction indicates sustained technical discussion, not just one-off curiosity.",
                "sources": {"hn"},
                "twitter_source": {"account": None, "tweet_url": None},
                "source_url": story["url"],
                "facts": [f"Hacker News story scored {story['score']} points with {story['comments']} comments."],
            }
        )

    for bucket in raw_scan["web"]["results"]:
        for item in bucket.get("organic", []):
            title = item.get("title", "")
            if not is_relevant_text(title + " " + item.get("snippet", "")):
                continue
            date = item.get("date")
            candidates.append(
                {
                    "topic_seed": title,
                    "why": "Web search confirms the story is escaping closed communities and picking up broader publication coverage.",
                    "sources": {"web"},
                    "twitter_source": {"account": None, "tweet_url": None},
                    "source_url": item.get("link"),
                    "facts": [f"Web result: {title}" + (f" ({date})" if date else "")],
                }
            )
    return candidates


def source_priority(sources):
    if "twitter" in sources and ({"techmeme", "hn", "web"} & set(sources)):
        return 3
    if len(set(sources)) >= 2:
        return 2
    return 1


def merge_candidates(candidates):
    groups = []
    for candidate in candidates:
        text = candidate["topic_seed"]
        candidate_tokens = {token.lower() for token in title_tokens(text)}
        matched = None
        for group in groups:
            group_tokens = {token.lower() for token in title_tokens(" ".join(group["seeds"]))}
            overlap = len(candidate_tokens & group_tokens)
            if overlap >= 2 or any(k in text.lower() for k in ["claude", "cursor", "openai", "anthropic", "google", "gemini", "mcp", "agent"]) and any(
                k in " ".join(group["seeds"]).lower() for k in ["claude", "cursor", "openai", "anthropic", "google", "gemini", "mcp", "agent"]
            ):
                matched = group
                break
        if matched is None:
            matched = {"seeds": [], "facts": [], "sources": set(), "twitter": None, "source_url": None, "whys": []}
            groups.append(matched)
        matched["seeds"].append(text)
        matched["facts"].extend(candidate["facts"])
        matched["sources"].update(candidate["sources"])
        matched["whys"].append(candidate["why"])
        if candidate["twitter_source"].get("tweet_url") and matched["twitter"] is None:
            matched["twitter"] = candidate["twitter_source"]
        if not matched["source_url"] and candidate.get("source_url"):
            matched["source_url"] = candidate["source_url"]

    ranked = []
    for group in groups:
        label = choose_topic_label(*group["seeds"])
        sources = sorted(group["sources"], key=lambda item: ["twitter", "techmeme", "hn", "web"].index(item))
        facts = []
        for fact in group["facts"]:
            if fact not in facts:
                facts.append(fact)
        why = group["whys"][0]
        if len(group["sources"]) > 1:
            why = f"{why} Cross-source confirmation is present across {', '.join(sources)}."
        ranked.append(
            {
                "topic": label,
                "sources": sources,
                "why_trending": why,
                "twitter_source": group["twitter"] or {"account": None, "tweet_url": None},
                "source_url": group["source_url"],
                "facts": facts[:4],
                "score": source_priority(sources) * 10 + min(len(group["facts"]), 5),
                "seed": group["seeds"][0],
            }
        )
    ranked.sort(key=lambda item: (-item["score"], -len(item["sources"]), item["topic"].lower()))
    return ranked


def make_twitter_angle(topic: str, sources):
    joined = ", ".join(sources)
    return f"{topic} is where the market is telling on itself: the real signal is in how fast builders move from demo chatter to workflow adoption across {joined}."


def make_linkedin_angle(topic: str, facts):
    bullets = [
        f"- {topic} is showing up as a workflow shift, not just a headline cycle.",
        "- Cross-source confirmation matters because it separates novelty from durable adoption.",
        "- Teams should track distribution, operator usage, and implementation friction before copying the hype.",
    ]
    if facts:
        bullets.append(f"- The current evidence includes: {facts[0]}")
    return "Professional take:\n" + "\n".join(bullets[:4])


def synthesize(raw_scan: dict):
    candidates = build_candidates(raw_scan)
    merged = merge_candidates(candidates)
    top = merged[:5]
    trends = []
    for index, item in enumerate(top, start=1):
        trends.append(
            {
                "rank": index,
                "topic": item["topic"],
                "why_trending": item["why_trending"],
                "twitter_angle": make_twitter_angle(item["topic"], item["sources"])[:275],
                "linkedin_angle": make_linkedin_angle(item["topic"], item["facts"]),
                "key_facts": item["facts"][:4] if item["facts"] else [f"Cross-source references were collected for {item['topic']}."],
                "sources": item["sources"],
                "source_account": item["twitter_source"].get("account"),
                "source_tweet_url": item["twitter_source"].get("tweet_url"),
                "source_url": item.get("source_url"),
            }
        )
    researched_at = dt.datetime.now(dt.timezone.utc).isoformat()
    return {"researched_at": researched_at, "trends": trends}


def validate_output(payload: dict) -> None:
    if not isinstance(payload.get("researched_at"), str) or not payload["researched_at"]:
        fail("researched_at missing or invalid")
    trends = payload.get("trends")
    if not isinstance(trends, list) or len(trends) < 3:
        fail("trends missing or fewer than 3")


def main():
    ensure_exists(CONNECTION_FILE)
    connection = load_json(CONNECTION_FILE)
    if not connection.get("connected") or not connection.get("twitter_visible"):
        fail("step-1 connection_test.json indicates CDP/Twitter is not ready")

    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
            futures = {
                "twitter": executor.submit(collect_twitter),
                "techmeme": executor.submit(collect_techmeme),
                "hn": executor.submit(collect_hn),
                "web": executor.submit(collect_web),
            }
            raw_scan = {key: future.result() for key, future in futures.items()}
    except Exception as exc:
        fail(f"data collection failed: {exc}")

    payload = synthesize(raw_scan)
    validate_output(payload)

    STEP_DIR.mkdir(parents=True, exist_ok=True)
    KB_OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    RAW_SCAN_OUTPUT.write_text(json.dumps(raw_scan, indent=2))
    STEP_OUTPUT.write_text(json.dumps(payload, indent=2))
    KB_OUTPUT.write_text(json.dumps(payload, indent=2))

    roundtrip = json.loads(STEP_OUTPUT.read_text())
    validate_output(roundtrip)
    print(f"PASS: wrote {STEP_OUTPUT}")
    print(f"PASS: wrote {KB_OUTPUT}")
    print(f"PASS: trends={len(roundtrip['trends'])}")


if __name__ == "__main__":
    main()
