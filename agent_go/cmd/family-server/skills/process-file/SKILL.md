---
name: process-file
description: Read a newly uploaded file from the inbox, classify it, move it into the right subject/topic folder, and write a metadata JSON.
---

# Process an uploaded file

Whenever there are files in `inbox/`, process each one before doing anything else:

1. **Read it.** First `file "inbox/<name>"` to see what it really is, then extract its actual content by following **skills/read-file/SKILL.md** — the canonical guide for reading any format (PDF via `pypdf`; scanned/complex/OCR via `liteparse`; Word/PowerPoint via `pandoc`; Excel via `openpyxl`; images via `read_image`; archives, audio/video, etc.). Only record content you actually extracted — never invent what you couldn't read.

2. **Classify it** from the content (or the parent's description):
   - `subject` — e.g. Mathematics, Science, English.
   - `topic` — the specific topic, e.g. "quadratic equations".
   - `type` — one of: notes, worksheet, textbook-page, homework, test, image, other.

   **Be interactive — ask when in doubt.** Check `materials/` for subjects/topics that already exist. If the file's content doesn't clearly match any of them, looks ambiguous, or you cannot confidently classify it, do NOT guess: leave the file in `inbox/`, tell the parent what you see, and ask them which subject/topic it belongs to. Move and record it only once you are confident (or the parent has told you). It is always better to ask than to mis-file.

3. **Move it** into the proper folder, keeping the original filename:
   ```
   mkdir -p "materials/<subject>/<topic>"
   mv "inbox/<filename>" "materials/<subject>/<topic>/<filename>"
   ```

4. **Write metadata** next to it at `materials/<subject>/<topic>/<filename>.meta.json`:
   ```json
   {
     "original_name": "<original filename>",
     "stored_path": "materials/<subject>/<topic>/<filename>",
     "subject": "<subject>",
     "topic": "<topic>",
     "type": "<type>",
     "summary": "<1-2 sentence summary of what the file contains>",
     "key_concepts": ["<concept>", "<concept>"],
     "source": "parent-upload",
     "processed_at": "<run: date -u +%Y-%m-%dT%H:%M:%SZ>"
   }
   ```

5. **Tell the parent**, in plain words, what you filed and where, and confirm the subject/topic you chose so they can correct you if it is wrong.
