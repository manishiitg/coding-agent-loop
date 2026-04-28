package instructions

// GetSpecialWorkspaceToolsInstructions returns a shared prompt section that
// explains the workspace-level generation and analysis tools used by both chat
// agents and workflow-builder agents.
func GetSpecialWorkspaceToolsInstructions() string {
	return `## Special Workspace Tools

Use these tools when you need a direct provider-backed capability instead of general chat reasoning:

- ` + "`generate_text_llm(user_message, tier)`" + ` — Generate text with one direct LLM call using the workspace tier config. ` + "`tier`" + ` must be ` + "`high`" + `, ` + "`medium`" + `, or ` + "`low`" + `.
- ` + "`search_web_llm(query, provider, model_id)`" + ` — Run a live web search using a published search-capable model from ` + "`config/published-llms.json`" + `. Both ` + "`provider`" + ` and ` + "`model_id`" + ` are required and must match a published entry.
- ` + "`image_gen(prompt, output_path, provider?)`" + ` — Generate images using ` + "`config/image-generation-config.json`" + ` or an explicit provider override. ` + "`output_path`" + ` is required and must be a workspace-relative destination chosen by the caller.
- ` + "`image_edit(image_path, output_path, prompt, provider?)`" + ` — Edit an existing workspace image. ` + "`output_path`" + ` is required and must be a workspace-relative destination chosen by the caller.
- ` + "`generate_video(prompt, output_path, model_id, provider?)`" + ` — Generate videos with Veo. ` + "`output_path`" + ` is required and must be a workspace-relative destination. ` + "`model_id`" + ` is required and determines the Google backend: Vertex AI models (` + "`veo-3.1-generate-001`" + `, ` + "`veo-3.1-lite-generate-001`" + `, ` + "`veo-3.1-fast-generate-001`" + `) require ` + "`GOOGLE_CLOUD_PROJECT`" + ` + ADC and support native audio; Gemini API preview models (` + "`veo-3.1-generate-preview`" + `, ` + "`veo-3.1-fast-generate-preview`" + `) use API-key auth and do not support native audio.
- ` + "`read_image(filepath, query)`" + ` — Analyze an image file using ` + "`config/image-analysis-config.json`" + `. If no image-analysis config exists, it falls back to the current chat model.
- ` + "`read_video(filepath, query, provider?)`" + ` — Analyze a workspace video file using Kimi ` + "`kimi-k2.6`" + ` by default, or Z.AI Vision MCP ` + "`video_analysis`" + ` with ` + "`provider=\"z-ai\"`" + `. Kimi uploads to Moonshot file storage and references ` + "`ms://<file-id>`" + `; Z.AI uses ` + "`npx -y @z_ai/mcp-server@latest`" + ` with local temporary files.

Provider setup rules:
- Keep provider auth in ` + "`config/provider-api-keys.json`" + ` using the ` + "`set_provider_auth`" + ` tool. Do not hand-edit the encrypted auth file.
- Search provider routing comes from ` + "`config/published-llms.json`" + `.
- Image generation defaults come from ` + "`config/image-generation-config.json`" + `.
- Image analysis defaults come from ` + "`config/image-analysis-config.json`" + `.
- Video analysis uses Kimi provider auth from ` + "`config/provider-api-keys.json`" + ` / ` + "`KIMI_API_KEY`" + ` by default. For Z.AI MCP video analysis, set provider auth for ` + "`z-ai`" + ` / ` + "`Z_AI_API_KEY`" + ` and pass ` + "`provider=\"z-ai\"`" + `.`
}
