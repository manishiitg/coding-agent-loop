---
name: process-file
description: Read a newly uploaded file from the inbox, classify it, move it into the right subject/topic folder, and write a metadata JSON.
---

# Process an uploaded file

Whenever there are files in `shared/inbox/`, process each one before doing anything else:

1. **Read it.** Use the shell — you are resourceful, so extract text from whatever format arrives. First run `file "shared/inbox/<name>"` to see what it is, then:
   - Text / Markdown / CSV / JSON: `cat` it.
   - PDF: try `pdftotext "<file>" -` (or `pdftotext -layout`). If the tool is missing, say so and ask the parent for a text version.
   - Word / PowerPoint / Excel (docx, pptx, xlsx): these are zip archives — `unzip -p "<file>" '*.xml' 2>/dev/null` and read the text out of the XML, or use a converter if one is installed (e.g. `libreoffice --headless --convert-to txt`, `pandoc`).
   - Zip / archives: `unzip -l` to list, then extract and process each file inside.
   - Images (photos of notes, worksheets, handwritten homework): open and read the image directly — you can see and understand images natively, so read the printed/handwritten text and any diagrams straight from it. Only if it is genuinely illegible, say so and ask the parent for a clearer photo.
   - Video / audio: you cannot watch/listen; record the filename and duration (`ffprobe` if available) and ask the parent what it covers.
   - Whatever the format, only record content you have actually extracted — never invent what you could not read.

2. **Classify it** from the content (or the parent's description):
   - `subject` — e.g. Mathematics, Science, English.
   - `topic` — the specific topic, e.g. "quadratic equations".
   - `type` — one of: notes, worksheet, textbook-page, homework, test, image, other.

   **Be interactive — ask when in doubt.** If the file's content does not clearly match the current subject/topic, looks like it belongs to a *different* subject than expected, is ambiguous, or you cannot confidently classify it, do NOT guess: leave the file in `shared/inbox/`, tell the parent what you see, and ask them which subject/topic it belongs to. Move and record it only once you are confident (or the parent has told you). It is always better to ask than to mis-file.

3. **Move it** into the proper folder, keeping the original filename:
   ```
   mkdir -p "shared/materials/<subject>/<topic>"
   mv "shared/inbox/<filename>" "shared/materials/<subject>/<topic>/<filename>"
   ```

4. **Write metadata** next to it at `shared/materials/<subject>/<topic>/<filename>.meta.json`:
   ```json
   {
     "original_name": "<original filename>",
     "stored_path": "shared/materials/<subject>/<topic>/<filename>",
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
