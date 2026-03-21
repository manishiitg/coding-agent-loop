# Draft Reply Sub-Task Learning

## Execution Workflow (Exact Mode)

### Optimal Path
1. `execute_shell_command`
   - arguments: `{ "command": "pwd" }`
   - prerequisite: confirm the shell root before assuming the repo path
   - output: the active workspace root, which in this run resolved to `/app/workspace-docs/Workflow/social-media`
   - on_error: if the declared cwd is unreachable, switch to the actual workspace root instead of forcing the documented path

2. `execute_shell_command`
   - arguments: `{ "command": "cd '/app/workspace-docs/Workflow/social-media' && find 'learnings/step-draft-reply-sub-task' -maxdepth 1 -name '*_learning.md' -type f | sort" }`
   - prerequisite: use the real workspace root
   - output: existing learning files for this sub-step, or an empty result if none exist
   - on_error: if the folder is empty, continue with the step artifacts rather than treating it as a failure

3. `execute_shell_command`
   - arguments: `{ "command": "cd '/app/workspace-docs/Workflow/social-media' && cat 'runs/iteration-30/manish/execution/step-5/tasks.md'" }`
   - prerequisite: step artifacts are present
   - output: task state showing the curated builder handles, the draft_reply delegation, and the verification/cleanup checklist
   - on_error: use the task file to recover the intended workflow if the live execution context is missing

4. `execute_shell_command`
   - arguments: `{ "command": "cd '/app/workspace-docs/Workflow/social-media' && cat 'runs/iteration-30/manish/execution/step-5/step_done.json'" }`
   - prerequisite: step completion metadata exists
   - output: exact completion shape with `completed_at`, `step_id`, `step_index`, and `step_path`
   - on_error: use this file to confirm the successful run before consolidating the learning note

5. `execute_shell_command`
   - arguments: `{ "command": "cd '/app/workspace-docs/Workflow/social-media' && find 'knowledgebase/research/builders' -maxdepth 1 -name '*.json' | sort" }`
   - prerequisite: the research bundle is already complete
   - output: the available builder research files
   - on_error: ignore unrelated historical JSON files and restrict work to the eight curated handles from the step instructions

6. `execute_shell_command`
   - arguments: `{ "command": "cd '/app/workspace-docs/Workflow/social-media' && cat '<research file path>'" }`
   - prerequisite: only the eight curated handles are in scope
   - output: each research JSON record containing `handle`, `twitter_handle`, `post_url`, `twitter_post_url`, and the research notes needed to draft the reply
   - on_error: skip any builder whose `twitter_post_url` is null and record it for exclusion

7. `diff_patch_workspace_file`
   - arguments: overwrite `knowledgebase/engagement/drafts/drafts_$(date +%Y-%m-%d).json` and `knowledgebase/engagement/drafted_engagements.json`
   - prerequisite: all valid research records have been read
   - output: a fresh JSON object with a top-level `drafts` array
   - on_error: do not generate the payload through Python or a hardcoded helper; write the final JSON directly

8. `execute_shell_command`
   - arguments: append skipped builders to `knowledgebase/engagement/skipped_builders.json` only when `twitter_post_url` is null
   - prerequisite: the existing skipped array has been read
   - output: updated skipped records shaped as `{ "handle": "...", "reason": "no_twitter_url", "skipped_at": "<ISO date>" }`
   - on_error: preserve previous entries and only append the new skipped builders

9. `execute_shell_command`
   - arguments: verify the written engagement files and then clean up extra `*_learning.md` files in `learnings/step-draft-reply-sub-task`
   - prerequisite: both engagement files are written successfully
   - output: only `Draft Reply Sub-Task_learning.md` remains in the learning folder
   - on_error: do not leave multiple learning files behind

### Data Flow
`tasks.md` and `step_done.json` -> identify the exact step state and success shape.

`knowledgebase/research/builders/<handle>.json` -> source of truth for `handle`, `twitter_handle`, `post_url`, `twitter_post_url`, and reply-grounding details.

Research JSON -> draft JSON payload -> both engagement output files.

Null `twitter_post_url` -> `skipped_builders.json` append-only update.

## Output File Formats

- `knowledgebase/engagement/drafts/drafts_$(date +%Y-%m-%d).json`
  - structure: `{ "drafts": [ { "handle": "string", "twitter_handle": "string", "post_url": "string", "twitter_post_url": "string", "drafted_reply": "string", "reply_type": "string", "reasoning": "string" } ] }`

- `knowledgebase/engagement/drafted_engagements.json`
  - structure: same as above

- `knowledgebase/engagement/skipped_builders.json`
  - structure: `[ { "handle": "string", "reason": "no_twitter_url", "skipped_at": "ISO-8601 string" } ]`

## Failures To Avoid

- `execute_shell_command` against the documented repo root when the shell actually starts at `/app/workspace-docs/Workflow/social-media`; use the real root first.
- discovery or research fan-out beyond the eight curated handles: `randal_olson`, `charlespacker`, `official_taches`, `SteOnChain`, `danshipper`, `askOkara`, `eachlabs`, `karpathy`.
- generating drafts with Python or a hardcoded script instead of reading the research files and writing the final JSON directly.
- including any builder whose `twitter_post_url` is null in the drafts payload.
- omitting `twitter_handle` or `twitter_post_url` from the draft output.
- leaving more than one `*_learning.md` file in `learnings/step-draft-reply-sub-task`.

## Success Criteria

- both engagement output files contain the same fresh JSON object
- the `drafts` array includes exactly the valid builders from the curated set
- every draft is grounded only in the corresponding research file
- builders with no `twitter_post_url` are recorded in `skipped_builders.json`
- the final learning folder contains only `Draft Reply Sub-Task_learning.md`
