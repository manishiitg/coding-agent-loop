package instructions

// GetSpecialWorkspaceToolsInstructions returns a shared prompt section that
// explains the workspace-level generation and analysis tools used by both chat
// agents and workflow-builder agents.
func GetSpecialWorkspaceToolsInstructions() string {
	return `## Special Workspace Tools

Use these tools when you need a direct provider-backed capability instead of general chat reasoning:

- ` + "`list_llm_capabilities(capability?, include_models?)`" + ` — Inspect which providers/models are supported and currently usable for ` + "`chat`" + `, ` + "`generate_image`" + `, ` + "`generate_video`" + `, ` + "`text_to_speech`" + `, ` + "`speech_to_text`" + `, and ` + "`generate_music`" + `. Use this before choosing a provider when the user's request depends on provider capability, auth, pricing, or runtime availability.
- ` + "`estimate_llm_cost(capability, provider, model_id?, characters?, seconds?, minutes?, count?)`" + ` — Estimate priced media generation/transcription costs before high-volume ` + "`generate_video`" + `, ` + "`text_to_speech`" + `, ` + "`speech_to_text`" + `, or ` + "`generate_music`" + ` runs.
- ` + "`set_provider_auth(provider, api_key?, region?, endpoint?, api_version?)`" + ` — Store provider auth in the encrypted workspace provider store. If the user provides an API key for Gemini/Vertex, MiniMax, ElevenLabs, Deepgram, or another managed provider, call this tool directly; do not paste the key into shell commands, scripts, curl calls, logs, or config files.
- ` + "`generate_text_llm(user_message, tier)`" + ` — Generate text with one direct LLM call using the workspace tier config. ` + "`tier`" + ` must be ` + "`high`" + `, ` + "`medium`" + `, or ` + "`low`" + `.
- ` + "`search_web_llm(query, provider, model_id)`" + ` — Run a live web search using a published search-capable model from ` + "`config/published-llms.json`" + `. Both ` + "`provider`" + ` and ` + "`model_id`" + ` are required and must match a published entry.
- ` + "`image_gen(prompt, output_path, provider?)`" + ` — Generate images using ` + "`config/image-generation-config.json`" + ` or an explicit provider override. ` + "`output_path`" + ` is required and must be a workspace-relative destination chosen by the caller.
- ` + "`image_edit(image_path, output_path, prompt, provider?)`" + ` — Edit an existing workspace image. ` + "`output_path`" + ` is required and must be a workspace-relative destination chosen by the caller.
- ` + "`generate_video(prompt, output_path, model_id, provider?)`" + ` — Generate videos with Veo. ` + "`output_path`" + ` is required and must be a workspace-relative destination. ` + "`model_id`" + ` is required and determines the Google backend: Vertex AI models (` + "`veo-3.1-generate-001`" + `, ` + "`veo-3.1-lite-generate-001`" + `, ` + "`veo-3.1-fast-generate-001`" + `) require ` + "`GOOGLE_CLOUD_PROJECT`" + ` + ADC and support native audio; Gemini API preview models (` + "`veo-3.1-generate-preview`" + `, ` + "`veo-3.1-fast-generate-preview`" + `) use API-key auth and do not support native audio.
- ` + "`text_to_speech(prompt, output_path, voice_name?, language_code?, provider?)`" + ` — Generate TTS speech audio with Gemini ` + "`gemini-3.1-flash-tts-preview`" + ` by default, MiniMax when ` + "`provider=\"minimax\"`" + `, ElevenLabs when ` + "`provider=\"elevenlabs\"`" + `, or Deepgram when ` + "`provider=\"deepgram\"`" + `. ` + "`output_path`" + ` is required and must be a workspace-relative destination. Use the prompt for style, pace, tone, accent, and the exact transcript to speak.
- ` + "`speech_to_text(audio_path, language_code?, provider?, model_id?)`" + ` — Transcribe workspace audio with Deepgram ` + "`nova-3`" + ` by default. ` + "`audio_path`" + ` is required and must be a workspace-relative source file.
- ` + "`generate_music(prompt, output_path, duration_ms?, instrumental?, provider?, model_id?)`" + ` — Generate music with ElevenLabs ` + "`music_v1`" + ` by default, or MiniMax when ` + "`provider=\"minimax\"`" + `. ` + "`output_path`" + ` is required and must be a workspace-relative destination. Use the prompt for genre, mood, instrumentation, structure, and lyrics direction.
- ` + "`read_image(filepath, query)`" + ` — Analyze an image file using ` + "`config/image-analysis-config.json`" + `. If no image-analysis config exists, it falls back to the current chat model.
- ` + "`read_video(filepath, query, provider?)`" + ` — Analyze a workspace video file using Kimi ` + "`kimi-k2.6`" + ` by default, or Z.AI Vision MCP ` + "`video_analysis`" + ` with ` + "`provider=\"z-ai\"`" + `. Kimi uploads to Moonshot file storage and references ` + "`ms://<file-id>`" + `; Z.AI uses ` + "`npx -y @z_ai/mcp-server@latest`" + ` with local temporary files.

Provider setup rules:
- Published LLM entries are for chat/text routing. Audio, video, image, and music providers are workspace tool capabilities; do not conclude they are unavailable just because they are absent from ` + "`config/published-llms.json`" + ` or a published-LLM list.
- For audio and music generation, call ` + "`text_to_speech`" + ` or ` + "`generate_music`" + ` directly. Do not hand-roll provider HTTP calls through ` + "`execute_shell_command`" + ` unless the dedicated workspace tool is unavailable and the user explicitly asks for raw API debugging.
- Keep provider auth in ` + "`config/provider-api-keys.json`" + ` using the ` + "`set_provider_auth`" + ` tool. Do not hand-edit the encrypted auth file.
- Do not read, cat, grep, print, or manually edit ` + "`config/provider-api-keys.json`" + `; it is encrypted and not useful to inspect as plaintext.
- Search provider routing comes from ` + "`config/published-llms.json`" + `.
- Image generation defaults come from ` + "`config/image-generation-config.json`" + `.
- Image analysis defaults come from ` + "`config/image-analysis-config.json`" + `.
- Video analysis uses Kimi provider auth from ` + "`config/provider-api-keys.json`" + ` / ` + "`KIMI_API_KEY`" + ` by default. For Z.AI MCP video analysis, set provider auth for ` + "`z-ai`" + ` / ` + "`Z_AI_API_KEY`" + ` and pass ` + "`provider=\"z-ai\"`" + `.`
}
