package instructions

// GetSpecialWorkspaceToolsInstructions returns the cheat-sheet section
// for workspace-level provider-backed tools (text/image/video/audio/music
// generation, image/video/PDF reading, transcription, web search, capability
// discovery). The full reference — signatures, parameters, defaults,
// provider-setup discipline — lives in the workspace-media-tools skill
// loaded on demand via get_reference_doc(kind="workspace-media-tools").
//
// Used by both chat agents and workflow-builder agents.
func GetSpecialWorkspaceToolsInstructions() string {
	return `## Special Workspace Tools (cheat sheet)

Provider-backed capabilities you can call directly instead of general chat reasoning. **Path contract**: every file-path argument must be a full absolute path under the workspace docs root. **Provider/model contract**: pass ` + "`provider`" + ` and ` + "`model_id`" + ` together from the same ` + "`list_llm_capabilities(capability=\"...\", include_models=true)`" + ` result — do not pass only ` + "`model_id`" + ` and ask the backend to infer.

Available tools:
- **Discovery + cost**: ` + "`list_llm_capabilities`" + `, ` + "`estimate_llm_cost`" + `, ` + "`set_provider_auth`" + ` (always use this for API keys — never paste into shell, scripts, or config files).
- **Text**: ` + "`generate_text_llm(user_message, tier)`" + ` · ` + "`search_web_llm(query, provider, model_id?)`" + `.
- **Image**: ` + "`image_gen(prompt, output_path, ...)`" + ` · ` + "`image_edit(image_path, output_path, prompt, ...)`" + `.
- **Video**: ` + "`generate_video(prompt, output_path, model_id, ...)`" + ` — Veo models (native audio) or Gemini Omni Flash (` + "`gemini-omni-flash-preview`" + `, native audio, fastest/720p-only).
- **Audio + music**: ` + "`text_to_speech`" + `, ` + "`speech_to_text`" + ` (default Deepgram nova-3), ` + "`generate_music`" + ` (default ElevenLabs music_v1).
- **Media reading**: ` + "`read_image`" + `, ` + "`read_video`" + `. No dedicated PDF tool — extract text with ` + "`execute_shell_command`" + ` + Python's ` + "`pypdf`" + `.

Provider-setup essentials (do not hand-edit provider-auth storage — it's encrypted and managed via ` + "`set_provider_auth`" + `; audio/video/image/music providers are workspace **tool** capabilities, not published-LLM entries — call ` + "`list_llm_capabilities(capability=\"...\")`" + ` for the authoritative availability answer).

These tools cover each provider's **basic** parameters only. For a capability a tool doesn't expose (e.g. multi-image composition, conversational editing, voice cloning), call ` + "`get_reference_doc(kind=\"workspace-media-tools\")`" + ` — it covers writing Python against the provider directly, including the credential gap (` + "`set_provider_auth`" + ` keys aren't shell-visible; use ` + "`set_workflow_secret`" + `/` + "`set_user_secret`" + ` instead).

**For the full reference — every tool's parameters, defaults, provider routing rules, model-ID lists, and common-mistake gotchas — call:** ` + "`get_reference_doc(kind=\"workspace-media-tools\")`" + `.`
}
