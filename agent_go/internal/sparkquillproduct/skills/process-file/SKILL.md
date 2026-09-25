---
name: process-file
description: File a parent upload into the right subject and topic with reusable extracted text.
---

# Process an uploaded file

Use this when the parent asks about an upload or the file matters to the
current task. Leave unrelated inbox files for later.

1. Identify and read the file with `read-file`. Record only content you actually
   extracted. Use the parent's description and existing `materials/` folders
   to choose a subject and topic; ask if classification is genuinely unclear.
2. Move the original into `materials/<subject>/<topic>/`, preserving its name.
   Write `<filename>.meta.json` beside it with `original_name`, `stored_path`,
   `subject`, `topic`, `type`, `summary`, `key_concepts`, `extracted_text`,
   `source` (`parent-upload`) and `processed_at` (UTC). Keep the full extracted
   text so later activities need not repeat OCR or transcription.
3. Tell the parent what was filed and where, so they can correct the category.
