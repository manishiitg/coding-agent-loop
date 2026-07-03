## Workspace Media & Provider Tools — Full Reference

This skill is the deep reference for workspace-level provider-backed
capabilities — text generation, image / video / audio / music
generation, image reading, transcription, web search.
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

## Basic tool vs advanced provider feature

`image_gen`/`image_edit`/`generate_video`/`text_to_speech`/`speech_to_text`/`generate_music`
each expose only the **common, basic parameters** of their underlying
provider — prompt, a handful of style/format knobs, one input file.
They do **not** expose every capability the provider actually has.
Examples: Gemini Omni Flash's multi-turn conversational video editing
(`previous_interaction_id`) exists at the provider API level but has no
parameter on `generate_video`;
ElevenLabs voice design/cloning, MiniMax's advanced music controls, and
provider-side video editing of an uploaded file are the same story.

**Use the tool for basic requests.** When the user's ask needs a
capability the tool doesn't expose, don't force it through the tool's
limited surface (e.g. stuffing instructions into the plain prompt and
hoping) — write Python via `execute_shell_command` and call the
provider's API directly (or use a matching skill, if one is installed
for that provider), the same way `read_pdf` was replaced by `pypdf`.

**The credential gap this creates:** provider keys stored via
`set_provider_auth` are consumed internally by the Go-side tools only —
they are **not** available to `execute_shell_command`. Only secrets set
via `set_workflow_secret`/`set_user_secret` are injected into the shell
as `SECRET_<NAME>`. So before writing provider API calls in Python,
check whether the needed key is already a secret; if the user only ever
ran `set_provider_auth`, ask them to also add it via `set_workflow_secret`/
`set_user_secret` — never ask them to paste the raw key into a shell
command or script.

## Tool reference

### Capability discovery + cost estimation

- **`list_llm_capabilities(capability?, include_models?)`** — Inspect which providers/models are supported and currently usable for `chat`, `search_web`, `read_image`, `generate_image`, `generate_video`, `text_to_speech`, `speech_to_text`, `generate_music`. Use this **before** choosing a provider when the user's request depends on provider capability, auth, pricing, or runtime availability.
- **`estimate_llm_cost(capability, provider, model_id?, characters?, seconds?, minutes?, count?)`** — Estimate priced media generation / transcription costs before high-volume `generate_video`, `text_to_speech`, `speech_to_text`, or `generate_music` runs.
- **`set_provider_auth(provider, api_key?, region?, endpoint?, api_version?)`** — Store provider auth in the encrypted workspace provider store. If the user provides an API key for Gemini/Vertex, MiniMax, ElevenLabs, Deepgram, or another managed provider, call this directly — **do not paste the key into shell commands, scripts, curl calls, logs, or config files**.

### Text generation

- **`generate_text_llm(user_message, tier)`** — Generate text with one direct LLM call using the workspace tier config. `tier` must be `high`, `medium`, or `low`.
- **`search_web_llm(query, provider, model_id?)`** — Run a live web search using a provider from the published search-capable LLM set. Before selecting a provider/model, call `list_llm_capabilities(capability="search_web", include_models=true)`. `provider` is required; `model_id` is optional only when accepting the backend's default for that provider.

### Image generation + editing

- **`image_gen(prompt, output_path, provider?, model_id?)`** — Generate images using workspace-backed image generation defaults or an explicit provider/model override from `list_llm_capabilities(capability="generate_image", include_models=true)`. `output_path` is required and must be a full absolute workspace-docs destination chosen by the caller.
- **`image_edit(image_path, output_path, prompt, provider?, model_id?)`** — Edit an existing workspace image using a provider/model pair from `list_llm_capabilities(capability="generate_image", include_models=true)`. `image_path` and `output_path` must be full absolute workspace-docs paths; use `absolute_paths` from a prior `image_gen` result when chaining.

### Video generation

- **`generate_video(prompt, output_path, model_id, provider?)`** — Generate videos using a provider/model pair from `list_llm_capabilities(capability="generate_video", include_models=true)`. `output_path` is required and must be a full absolute workspace-docs destination. `input_image_path` and `last_frame_path`, when used, must also be absolute. Use `last_frame` / `last_frame_path` with `input_image` / `input_image_path` for Veo first-frame/last-frame interpolation; the current Gemini Omni tool path rejects `last_frame`. `model_id` determines the Google backend:
  - **Vertex AI Veo** (`veo-3.1-generate-001`, `veo-3.1-lite-generate-001`, `veo-3.1-fast-generate-001`) requires `GOOGLE_CLOUD_PROJECT` + ADC and supports native audio.
  - **Gemini API preview Veo** (`veo-3.1-generate-preview`, `veo-3.1-fast-generate-preview`) uses API-key auth and does **not** support native audio.
  - **Gemini Omni Flash** (`gemini-omni-flash-preview`) uses API-key auth, is 720p-only, 3-10s clips, fastest to generate, includes native audio, and always produces exactly 1 video per call regardless of `number_of_videos`. Multi-turn conversational editing exists on the provider but is not exposed by this tool — see "Basic tool vs advanced provider feature" above.

### Audio + music

- **`text_to_speech(prompt, output_path, voice_name?, language_code?, provider?, model_id?)`** — Generate TTS speech audio using a provider/model pair from `list_llm_capabilities(capability="text_to_speech", include_models=true)`. Defaults to Gemini `gemini-3.1-flash-tts-preview`, MiniMax when `provider="minimax"`, ElevenLabs when `provider="elevenlabs"`, or Deepgram when `provider="deepgram"`. `output_path` is required and must be a full absolute workspace-docs destination. Use the prompt for style, pace, tone, accent, and the exact transcript to speak.
- **`speech_to_text(audio_path, language_code?, provider?, model_id?)`** — Transcribe workspace audio using a provider/model pair from `list_llm_capabilities(capability="speech_to_text", include_models=true)`. Defaults to Deepgram `nova-3`. `audio_path` is required and must be a full absolute workspace-docs source file.
- **`generate_music(prompt, output_path, duration_ms?, instrumental?, provider?, model_id?)`** — Generate music using a provider/model pair from `list_llm_capabilities(capability="generate_music", include_models=true)`. Defaults to ElevenLabs `music_v1`, or MiniMax when `provider="minimax"`. `output_path` is required and must be a full absolute workspace-docs destination. Use the prompt for genre, mood, instrumentation, structure, and lyrics direction.

### Media + document reading

- **`read_image(filepath, query, provider?, model_id?)`** — Analyze an image file using a provider/model pair from `list_llm_capabilities(capability="read_image", include_models=true)` or workspace-backed image analysis defaults. `filepath` must be a full absolute path under the workspace docs root; do not pass workspace-relative paths. If no image-analysis defaults exist, it falls back to the current chat model. `codex-cli`, `cursor-cli`, and `claude-code` are supported by passing the local workspace image path to the CLI; `agy-cli` is deprecated and only retained for existing legacy defaults.
- **Videos and PDFs have no dedicated reading tool.** Inspect videos with local `execute_shell_command` workflows such as frame/audio extraction, and extract PDF text with Python's `pypdf` (already installed) — no provider round-trip needed for plain text extraction.

## Provider setup rules

- **Published LLM entries are for chat / text routing only.** Audio, video, image, and music providers are workspace **tool** capabilities; do not conclude they are unavailable just because they are absent from a published-LLM list.
- **For audio and music that fit the tool's basic parameters**, call `text_to_speech` or `generate_music` **directly** rather than hand-rolling the same request through `execute_shell_command` + curl. Once the ask needs a capability the tool doesn't expose (see "Basic tool vs advanced provider feature" above), Python calling the provider directly is the expected path, not a fallback of last resort — just don't reimplement what the tool already does well for a basic request.
- **Provider auth** is managed via the `set_provider_auth` tool. Do not read or hand-edit encrypted config files.
- **Do not read, cat, grep, print, or manually edit config auth files** — they are encrypted and not useful to inspect as plaintext.
- **Search provider routing** comes from the published LLM set surfaced by `list_published_llms`.
- **Image generation defaults** come from workspace-backed image generation config; override per call with explicit provider/model when needed.
- **Image analysis defaults** come from workspace-backed image analysis config; override per call with explicit provider/model when needed.
- **Video analysis** is not exposed as a built-in workspace tool right now. Use local extraction or provider-specific scripts only when the needed credentials are available as workflow/user secrets.

## Common mistakes

- **Workspace-relative paths**: `Downloads/foo.pdf` instead of the full absolute path. The tools reject these.
- **Passing `model_id` without `provider`**: ambiguous routing. Always pair them from the same `list_llm_capabilities` result.
- **Typing a raw API key value into a shell command or script**: leaks into logs / scrollback. Store it via `set_provider_auth` for the built-in tools, or `set_workflow_secret`/`set_user_secret` for Python that needs it as `SECRET_<NAME>` — never the literal value.
- **Editing provider-auth config by hand**: auth is encrypted; manual edits corrupt it.
- **Reaching for `execute_shell_command` + curl/Python to redo a basic request** the dedicated tool already handles well: you lose its auth wiring, retries, output validation, and cost tracking for no benefit. Reserve the Python path for capabilities the tool genuinely doesn't expose.
- **Assuming a provider is unavailable** because it doesn't show up in the published LLM set: published LLMs are for chat/search routing, not media tools. Call `list_llm_capabilities(capability="...")` for the authoritative answer.
