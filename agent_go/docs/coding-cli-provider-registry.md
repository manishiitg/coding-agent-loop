# Coding-CLI Provider Registry Migration (pending)

Goal: `multi-llm-provider-go` is the single source of truth for provider
facts. `agent_go` must derive, not duplicate. Rule for new providers
(e.g. `muse-cli`): add the contract + adapter in `multi-llm-provider-go`
first, re-export via `mcpagent/llm`, then wire product policy in `agent_go`.
Never copy a provider string list into `agent_go`.

## Pending
- [ ] Replace hardcoded per-provider model alias lists
  (`cmd/server/llm_config_handlers.go:535`) with a uniform provider-repo
  accessor (needs new `CodingAgentModels(provider)` in
  `multi-llm-provider-go`, re-exported via `mcpagent/llm`; per-adapter
  `GetAllXxxModels()` already exist).
- [ ] Route the transcript-restore switch
  (`cmd/server/claude_native_transcript_sync.go:241`,
  `chat_history_persistence.go:2593`) through the provider-repo
  `transcriptReaderRegistry` / `NativeResumeOption`.
- [ ] Derive the workflow P0 matrix
  (`pkg/orchestrator/types/coding_cli_p0_workflow_e2e_real_test.go:53`)
  from `llm.CodingAgentProviderContracts()` instead of asserting exactly
  four — new entries go red until their P0 proofs land (forcing function).
- [ ] Keep in `agent_go` (product policy, explicit opt-in per surface):
  offering/visibility, Cursor-structured default
  (`cmd/server/coding_agent_modes.go`), tier-to-model mapping, auth UX copy.
  New providers fail closed here.
- [ ] Add `muse-cli` row to provider `docs/providers.md` once its stub lands.

## In Progress

- [ ] Muse (`muse-cli`) step-1 stub: provider-repo half APPLIED
  (uncommitted working tree: contract entry, init, `musecli` adapter stub;
  P0 gate red as designed — see below). `mcpagent` re-export DEFERRED:
  it pins the published provider version, so the one-liner
  (`/tmp/muse-stub/mcpagent.patch`) lands only with the provider version
  bump, else `llm` breaks. Next: `fresh_launch` tmux smoke, then work the
  P0 failure list. Recon doc staged at
  `/tmp/MUSE_CLI_CODING_AGENT_CONTRACT.md`.
- [ ] Muse (`muse-cli`) recon (done, see staged doc above) — see staged
  `MUSE_CLI_CODING_AGENT_CONTRACT.md` (to be moved to
  `multi-llm-provider-go/docs/`). Verified: `exec --json` event shape,
  `--session-id` resume, sidecar
  `$XDG_DATA_HOME/muse/sessions/YYYY/MM/DD/<uuid>/session.jsonl`,
  `META_API_KEY` priority, untrusted-by-default workspace.
  TBD live: tmux smoke, usage fields in session.jsonl, bridge-only flag,
  project skill dir (`.muse/skills/` ruled out), user rules path.

## Completed

- [x] `isPublishedLLMProviderAllowed` (`cmd/server/llm_config_handlers.go`)
  derives from `llm.ValidateProvider` instead of a hardcoded 9-provider
  switch (pinned by
  `TestIsPublishedLLMProviderAllowedDerivesFromRegistry`). Widens the gate
  to the shared set: adds openrouter, z-ai, kimi, minimax,
  minimax-coding-plan; elevenlabs/deepgram/empty/unknown still rejected.
  `supportedLLMProviders` offering list and deprecation filter unchanged.
  Duplicate in `cmd/server/services/workspace_config.go:41` intentionally
  left (separate package, same hardcoded list — candidate for the same
  treatment).
