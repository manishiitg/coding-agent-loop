## Workspace Media & Provider Tools — Full Reference

This skill is the deep reference for workspace-level provider-backed
capabilities — text generation, image / video / audio / music
generation, image / video / PDF reading, transcription, web search.
The inline system prompt now carries only a one-line-per-tool cheat
sheet; this doc has the full signatures, parameters, defaults,
provider routing rules, and provider-setup discipline.

## Path contract

Every file-path argument for these tools must be a **full absolute path
under the workspace docs root**. Do NOT pass workspace-relative paths to
media read, generation, edit, transcription, or PDF tools. When chaining
(e.g. feeding `image_gen` output into `image_edit`), use the
`absolute_paths` field from the prior tool result.

## Provider/model contract

When you choose a concrete model for any provider-backed LLM or media
tool, pass **`provider` and `model_id` together** from the same
`list_llm_capabilities(capability="...", include_models=true)` result.
Do not pass only `model_id` and ask the backend to infer the provider —
ambiguous routing leads to wrong-provider calls.

## Tool reference

### Capability discovery + cost estimation

- **`list_llm_capabilities(capability?, include_models?)`** — Inspect which providers/models are supported and currently usable for `chat`, `search_web`, `read_image`, `read_video`, `generate_image`, `generate_video`, `text_to_speech`, `speech_to_text`, `generate_music`. Use this **before** choosing a provider when the user's request depends on provider capability, auth, pricing, or runtime availability.
- **`estimate_llm_cost(capability, provider, model_id?, characters?, seconds?, minutes?, count?)`** — Estimate priced media generation / transcription costs before high-volume `generate_video`, `text_to_speech`, `speech_to_text`, or `generate_music` runs.
- **`set_provider_auth(provider, api_key?, region?, endpoint?, api_version?)`** — Store provider auth in the encrypted workspace provider store. If the user provides an API key for Gemini/Vertex, MiniMax, ElevenLabs, Deepgram, or another managed provider, call this directly — **do not paste the key into shell commands, scripts, curl calls, logs, or config files**.

### Text generation

- **`generate_text_llm(user_message, tier)`** — Generate text with one direct LLM call using the workspace tier config. `tier` must be `high`, `medium`, or `low`.
- **`search_web_llm(query, provider, model_id?)`** — Run a live web search using a provider from the published search-capable LLM set. Before selecting a provider/model, call `list_llm_capabilities(capability="search_web", include_models=true)`. `provider` is required; `model_id` is optional only when accepting the backend's default for that provider.

### Image generation + editing

- **`image_gen(prompt, output_path, provider?, model_id?)`** — Generate images using workspace-backed image generation defaults or an explicit provider/model override from `list_llm_capabilities(capability="generate_image", include_models=true)`. `output_path` is required and must be a full absolute workspace-docs destination chosen by the caller.
- **`image_edit(image_path, output_path, prompt, provider?, model_id?)`** — Edit an existing workspace image using a provider/model pair from `list_llm_capabilities(capability="generate_image", include_models=true)`. `image_path` and `output_path` must be full absolute workspace-docs paths; use `absolute_paths` from a prior `image_gen` result when chaining.

### Video generation

- **`generate_video(prompt, output_path, model_id, provider?)`** — Generate videos with Veo using a provider/model pair from `list_llm_capabilities(capability="generate_video", include_models=true)`. `output_path` is required and must be a full absolute workspace-docs destination. `input_image_path`, when used, must also be absolute. `model_id` determines the Google backend:
  - **Vertex AI** (`veo-3.1-generate-001`, `veo-3.1-lite-generate-001`, `veo-3.1-fast-generate-001`) requires `GOOGLE_CLOUD_PROJECT` + ADC and supports native audio.
  - **Gemini API preview** (`veo-3.1-generate-preview`, `veo-3.1-fast-generate-preview`) uses API-key auth and does **not** support native audio.

### Audio + music

- **`text_to_speech(prompt, output_path, voice_name?, language_code?, provider?, model_id?)`** — Generate TTS speech audio using a provider/model pair from `list_llm_capabilities(capability="text_to_speech", include_models=true)`. Defaults to Gemini `gemini-3.1-flash-tts-preview`, MiniMax when `provider="minimax"`, ElevenLabs when `provider="elevenlabs"`, or Deepgram when `provider="deepgram"`. `output_path` is required and must be a full absolute workspace-docs destination. Use the prompt for style, pace, tone, accent, and the exact transcript to speak.
- **`speech_to_text(audio_path, language_code?, provider?, model_id?)`** — Transcribe workspace audio using a provider/model pair from `list_llm_capabilities(capability="speech_to_text", include_models=true)`. Defaults to Deepgram `nova-3`. `audio_path` is required and must be a full absolute workspace-docs source file.
- **`generate_music(prompt, output_path, duration_ms?, instrumental?, provider?, model_id?)`** — Generate music using a provider/model pair from `list_llm_capabilities(capability="generate_music", include_models=true)`. Defaults to ElevenLabs `music_v1`, or MiniMax when `provider="minimax"`. `output_path` is required and must be a full absolute workspace-docs destination. Use the prompt for genre, mood, instrumentation, structure, and lyrics direction.

### Media + document reading

- **`read_image(filepath, query, provider?, model_id?)`** — Analyze an image file using a provider/model pair from `list_llm_capabilities(capability="read_image", include_models=true)` or workspace-backed image analysis defaults. `filepath` must be a full absolute path under the workspace docs root; do not pass workspace-relative paths. If no image-analysis defaults exist, it falls back to the current chat model. `codex-cli`, `cursor-cli`, and `claude-code` are supported by passing the local workspace image path to the CLI; `agy-cli` is deprecated and only retained for existing legacy defaults.
- **`read_video(filepath, query, provider?, model_id?)`** — Analyze a workspace video file using a provider/model pair from `list_llm_capabilities(capability="read_video", include_models=true)`. `filepath` must be a full absolute workspace-docs path. Direct video providers are not advertised by default; prefer a published coding-agent model until a dedicated provider is configured.
- **`read_pdf(filepath, page_range?, max_pages?, password?)`** — Extract text from a workspace PDF. `filepath` must be a full absolute workspace-docs path.

## Provider setup rules

- **Published LLM entries are for chat / text routing only.** Audio, video, image, and music providers are workspace **tool** capabilities; do not conclude they are unavailable just because they are absent from a published-LLM list.
- **For audio and music**, call `text_to_speech` or `generate_music` **directly**. Do not hand-roll provider HTTP calls through `execute_shell_command` unless the dedicated workspace tool is unavailable AND the user explicitly asks for raw API debugging.
- **Provider auth** is managed via the `set_provider_auth` tool. Do not read or hand-edit encrypted config files.
- **Do not read, cat, grep, print, or manually edit config auth files** — they are encrypted and not useful to inspect as plaintext.
- **Search provider routing** comes from the published LLM set surfaced by `list_published_llms`.
- **Image generation defaults** come from workspace-backed image generation config; override per call with explicit provider/model when needed.
- **Image analysis defaults** come from workspace-backed image analysis config; override per call with explicit provider/model when needed.
- **Video analysis** uses Kimi provider auth / `KIMI_API_KEY` by default. For Z.AI MCP video analysis, set provider auth for `z-ai` / `Z_AI_API_KEY` and pass `provider="z-ai"`.

## Common mistakes

- **Workspace-relative paths**: `Downloads/foo.pdf` instead of the full absolute path. The tools reject these.
- **Passing `model_id` without `provider`**: ambiguous routing. Always pair them from the same `list_llm_capabilities` result.
- **Pasting API keys in shell**: leaks into logs / scrollback. Always use `set_provider_auth`.
- **Editing provider-auth config by hand**: auth is encrypted; manual edits corrupt it.
- **Reaching for `execute_shell_command` + `curl` for audio/music**: the dedicated tools exist for a reason — auth, retries, output validation, cost tracking. Use them.
- **Assuming a provider is unavailable** because it doesn't show up in the published LLM set: published LLMs are for chat/search routing, not media tools. Call `list_llm_capabilities(capability="...")` for the authoritative answer.
