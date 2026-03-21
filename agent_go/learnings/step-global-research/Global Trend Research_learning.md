---
name: Global Trend Research Learning
description: Replayable workflow for unified trend research across Twitter CDP, Techmeme, Hacker News, and native web search.
type: project
---

## EXECUTION WORKFLOW

### 1. Run all sources in parallel inside one Python script

Use a single step-local Python runner. Do not sequence the sources manually.

Write outputs to:
- `{{WORKSPACE_PATH}}/runs/iteration-30/manish/execution/step-3/raw_scan.json`
- `{{WORKSPACE_PATH}}/runs/iteration-30/manish/execution/step-3/global_trends.json`
- `{{WORKSPACE_PATH}}/knowledgebase/research/global_trends.json`

### 2. Twitter CDP scan

Use the shared browser CDP endpoint:

```python
def ab(*args):
    return subprocess.run(
        ['agent-browser', '--cdp', '{{TWITTER_CDP_URL}}', *args],
        capture_output=True,
        text=True,
    ).stdout
```

Collect both Explore trends and profile tweets.

#### Explore

```python
ab('open', 'https://x.com/explore?tab=trending')
time.sleep(3)
explore_raw = ab('eval', '''
JSON.stringify(
  Array.from(document.querySelectorAll('[data-testid="trend"]')).slice(0,10).map(el => ({
    topic: el.querySelector('[dir="ltr"]')?.innerText || el.innerText.split("\\n")[0],
    posts: el.innerText.match(/([\\d.]+K?) posts/)?.[1] || null
  }))
)
''')
```

If that returns `[]`, try the fallback selector:

```python
ab('eval', '''
JSON.stringify(
  Array.from(document.querySelectorAll('div[aria-label] > div > div')).filter(el =>
    el.innerText.includes("posts") || el.innerText.includes("trending")
  ).slice(0,10).map(el => ({topic: el.innerText.split("\\n")[0], posts: null}))
)
''')
```

If both are empty, record:

```json
{"error":"extraction_failed","trends":[]}
```

Do not invent counts.

#### Profiles

Scan these handles:

```python
HANDLES = ['karpathy', 'simonw', 'swyx', 'GergelyOrosz', 'levelsio', 'dhh', 'emollick', 't3dotgg']
```

For each handle:

```python
ab('open', f'https://x.com/{handle}')
time.sleep(2)
posts_raw = ab('eval', '''
JSON.stringify(
  Array.from(document.querySelectorAll('[data-testid="tweet"]'))
    .filter(t => !t.querySelector('[data-testid="socialContext"]'))
    .slice(0,5)
    .map(t => ({
      text: t.querySelector('[data-testid="tweetText"]')?.innerText || '',
      url: t.querySelector('a[href*="/status/"]')?.href || ''
    }))
)
''')
```

Key rule: skip pinned tweets with `[data-testid="socialContext"]`.

### 3. Techmeme

Techmeme is JS-rendered. Use CDP, not curl.

```python
ab('open', 'https://www.techmeme.com/')
time.sleep(3)
headlines_raw = ab('eval', """
JSON.stringify(
  Array.from(document.querySelectorAll('.ourh, .hl')).slice(0,15).map(el => ({
    text: el.innerText.trim().substring(0,150),
    href: el.href || el.querySelector('a')?.href || null
  }))
)
""")
```

Normalize the result before use. In the successful run, `agent-browser eval` returned JSON as a string, not a native object.

### 4. Hacker News

Use stdlib `urllib.request` and `concurrent.futures`.

```python
import urllib.request, json, concurrent.futures

with urllib.request.urlopen('https://hacker-news.firebaseio.com/v0/topstories.json') as r:
    ids = json.loads(r.read())[:30]

def fetch_story(sid):
    with urllib.request.urlopen(f'https://hacker-news.firebaseio.com/v0/item/{sid}.json') as r:
        return json.loads(r.read())

with concurrent.futures.ThreadPoolExecutor(max_workers=10) as ex:
    stories = list(ex.map(fetch_story, ids))

relevant = [
    s for s in stories
    if s
    and s.get('score', 0) > 100
    and s.get('descendants', 0) > 20
    and any(
        kw in (s.get('title', '') + s.get('url', '')).lower()
        for kw in ['ai', 'llm', 'agent', 'model', 'code', 'dev', 'open source', 'api']
    )
]
```

Filter rule is strict: `score > 100 AND descendants > 20`.

### 5. Web search

Use the native web search capability from the tool surface. Do not call an external search API.

Target queries:
- `AI developer tools trending today March 2026`
- `open source AI agents news this week`
- `developer tools launches this week`

In the successful run, the web-search response also needed JSON normalization because the payload could arrive as a string.

### 6. Synthesis

Cross-reference the four sources and pick the top 5 trends.

Priority rule:
- Twitter + any of Techmeme, HN, or web = highest priority
- Single-source stories rank lower unless Twitter-sourced with a named account and real tweet URL

Do not fabricate `source_tweet_url`. Use `null` when there is no real Twitter source.

Recommended synthesis themes that matched the successful run:
- AI coding tools as a workflow and pricing war
- Cursor Composer 2 trust and model-disclosure risk
- Open-source coding agents gaining legitimacy
- GPU scarcity and agent infrastructure tightening together
- AI governance shifting from principle to operating constraint

### 7. Validation and persistence

Write a single `global_trends.json` object with this shape:

```json
{
  "researched_at": "ISO8601 string",
  "trends": [
    {
      "rank": 1,
      "topic": "string",
      "why_trending": "string",
      "twitter_angle": "string",
      "linkedin_angle": "string",
      "key_facts": ["string", "string", "string"],
      "sources": ["twitter", "techmeme", "hn", "web"],
      "source_account": "@handle or null",
      "source_tweet_url": "https://x.com/... or null",
      "source_url": "https://... or null"
    }
  ]
}
```

Write `raw_scan.json` with this shape:

```json
{
  "twitter": {
    "explore": {"trends": [], "error": null},
    "profiles": {"karpathy": []}
  },
  "hn": {
    "top_story_ids": [],
    "relevant_stories": []
  },
  "web": {
    "results": []
  }
}
```

Final validation should confirm:
- `researched_at` is present
- `trends` contains 5 items
- each trend has `twitter_angle`, `linkedin_angle`, `key_facts`, and `sources`

### 8. Failure modes to avoid

- `agent-browser --session` instead of `--cdp {{TWITTER_CDP_URL}}`
- `--snapshot -i` on Twitter
- Assuming `[data-testid="trend"]` will always return data
- Treating `agent-browser eval` output as already-parsed JSON
- Treating the web-search response as already-parsed JSON
- Using OR instead of AND for HN filtering
- Reusing a fabricated `source_tweet_url`
- Leaving pinned tweets in handle scans

### 9. Cleanup

Keep only this final learning file in `learnings/step-global-research`. Remove any other `*_learning.md` files in that folder.
