---
name: Twitter Fast Hot Take Drafter Learning
description: Optimal execution sequence for drafting multi-trend Twitter hot takes from selected_trends.json plus global trend detail, live tweet context, and current hashtag research
type: project
---

## EXECUTION WORKFLOW (EXACT MODE)

### OPTIMAL PATH

1. `execute_shell_command` - read `selected_trends.json`
   - arguments: `{ "command": "cat '{{WORKSPACE_PATH}}/execution/step-{{SELECTED_STEP}}/selected_trends.json'" }`
   - prerequisites: `selected_trends.json` exists from the prior selection step
   - outputs: selected trend list with the chosen `rank` values and/or topic references
   - on_error: check the step offset first; in one run the file was under `step-18`, not the step named in the prompt

2. `execute_shell_command` - read `global_trends.json` and match by `rank`
   - arguments: `{ "command": "cat '{{WORKSPACE_PATH}}/execution/step-{{GLOBAL_STEP}}/global_trends.json'" }`
   - prerequisites: `selected_trends.json` is already read
   - outputs: full trend objects with `rank`, `topic`, `twitter_angle`, `key_facts`, and `source_tweet_url`
   - on_error: if `global_trends.json` is missing in the mounted tree, fall back to the repo's available trend artifact that already contains the same fields, then continue by matching selected `rank`s

3. `execute_shell_command` with `agent-browser` - open source tweets when available
   - arguments: `{ "command": "agent-browser --cdp {{TWITTER_CDP_URL}} navigate --url '{{SOURCE_TWEET_URL}}' --wait networkidle" }`
   - prerequisites: `source_tweet_url` is non-null for the matched trend
   - outputs: snapshot of the original tweet plus visible replies
   - on_error: if replies are blocked by an auth modal, take the tweet snapshot and continue; do not stall the workflow
   - note: read the original tweet and the top 3-5 replies for debate context

4. `web_search` via MiniMax - research current hashtags for the topic
   - arguments: query the topic with hashtag-focused searches aimed at what real people are posting right now; if the first pass is too broad, tighten it to X-style hashtag usage
   - prerequisites: topic is known from the matched trend
   - outputs: 2-3 real hashtags used in recent posts for that topic
   - on_error: if the first query returns broad topic coverage but no usable tags, refine the search instead of inventing tags

5. `execute_shell_command` - write `twitter_drafts.json`
   - arguments: `{ "command": "cat > '{{WORKSPACE_PATH}}/execution/step-{{CURRENT_STEP}}/twitter_drafts.json' << 'EOF'\n[...]\nEOF" }`
   - prerequisites: selected ranks matched to full trend objects; live tweet context gathered when available; hashtags researched
   - outputs: `twitter_drafts.json` array with one object per selected trend
   - on_error: keep the payload valid JSON; preserve the exact array shape below

---

## DATA FLOW

```text
selected_trends.json
  -> selected ranks / topic references

global_trends.json
  -> matched rank entry
  -> topic, twitter_angle, key_facts, source_tweet_url

source_tweet_url (if non-null)
  -> agent-browser CDP snapshot
  -> original tweet + top replies

MiniMax hashtag search
  -> 2-3 current real hashtags

All inputs
  -> opinionated draft generation
  -> twitter_drafts.json
```

---

## OUTPUT FILE FORMAT

**File**: `twitter_drafts.json`
```json
[
  {
    "rank": 1,
    "topic": "string",
    "source_tweet_url": "string url or null",
    "draft_thread": ["tweet 1", "tweet 2 optional"],
    "draft_single": "string under 280 chars",
    "reasoning": "string explaining the angle and target reaction"
  }
]
```

### Draft rules that mattered in execution
- Start from `twitter_angle` in `global_trends.json`, but rewrite it in your own voice
- Lead with your opinion, not a summary of the trend
- Keep each tweet under 280 chars
- Lowercase is fine
- Be specific: name the tool, model, claim, or competitor
- Controversial beats safe
- No AI-speak or hedging
- Add 2-3 real hashtags to the end of both `draft_thread` tweets and the `draft_single` tweet
- Write both a thread version and a single version for every selected trend

---

## FAILURES TO AVOID

- Using the wrong step offset for `selected_trends.json`; the file may not be in the step named in the prompt
- Assuming `global_trends.json` is always present; fall back to the available trend artifact if needed, then match by `rank`
- Passing `--session` to `agent-browser`; only the bare `agent-browser --cdp {{TWITTER_CDP_URL}} ...` form worked
- Waiting forever on a reply list when X shows an auth modal; take the tweet snapshot and move on
- Skipping hashtag research or making up hashtags instead of pulling 2-3 current ones from live search
- Omitting either `draft_thread` or `draft_single`; both are required
- Forgetting to append hashtags to both draft variants in the final JSON
- Relying only on `twitter_drafts.json` when the validator checks for the singular `twitter_draft.json`; write the compatibility copy as well if the step harness expects it
