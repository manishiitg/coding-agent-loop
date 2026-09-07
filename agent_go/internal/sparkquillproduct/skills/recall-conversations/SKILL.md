---
name: recall-conversations
description: Read what was said in an earlier conversation — the parent's previous chats with Quill, or a child's conversation with the tutor inside an activity — from the platform's conversation logs.
---

# Recall an earlier conversation

Every conversation is logged by the platform, one JSON file per conversation,
under the family's chat-history folder — two levels up from your own folder:

```
../../chat_history/<yyyy-mm-dd>/session-<session-id>-conversation.json
```

The date folder is the day the conversation started; a conversation that ran
over several days stays in its first day's folder. `updated_at` inside the file
is its last activity. Newest first is `ls -t ../../chat_history/*/session-*-conversation.json`.

## Which file is which

- **An activity's conversation** (the child and the tutor): the activity's
  `product.json` holds its `session_id`:
  ```
  sid=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['session_id'])" "activities/<slug>/product.json")
  ls ../../chat_history/*/session-${sid}-conversation.json
  ```
  An activity from before September 2026 keeps its older log as
  `legacy-conversation.json` inside the activity folder instead (a plain
  `messages` list) — read that one directly.
- **The parent's earlier chats with you**: the files whose `runtime.workspace_path`
  ends in `Chats/SparkQuill` (an activity's ends in `activities/<slug>`). The
  current chat is already in front of you — its session id is the
  `sparkquill/main` entry of `../../chat_history/product-conversations.json`; skip
  that file, the earlier ones are the previous chats.
- WhatsApp turns run inside these same conversations; there is no separate log.

## Reading a log

`conversation_history` is a list of turns, each `{"Role": ..., "Parts": [...]}`.
`Role` is `human` (the parent, or the child in an activity), `ai` (you, or the
tutor), `tool` (a tool result) or `system`. Only text parts matter for recall:

```
python3 - "../../chat_history/2026-09-06/session-<id>-conversation.json" <<'EOF'
import json, sys
d = json.load(open(sys.argv[1]))
for turn in d.get("conversation_history", []):
    if turn.get("Role") not in ("human", "ai"):
        continue
    text = " ".join(p["Text"] for p in turn.get("Parts", []) if isinstance(p, dict) and p.get("Text"))
    if text.strip():
        print(f"[{turn['Role']}] {text.strip()}\n")
EOF
```

A long conversation is long — pipe through `head`, or grep for the topic the
parent asked about, before reading the whole thing.

## Rules

- Recall is for answering a real question ("what did we decide about the
  fractions test?", "how did she do on Tuesday?", "what did she say about the
  story?"), for the progress page and for the check-in — not something to do
  every turn.
- Tell the parent what was said in your own words; quote a line only when they
  ask what someone actually said. Never mention files, folders, session ids or
  JSON — "in your chat last Tuesday" is the whole reference.
- Read; never edit or delete a log. The platform owns these files.
