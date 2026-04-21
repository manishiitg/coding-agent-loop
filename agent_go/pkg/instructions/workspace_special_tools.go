package instructions

// GetSpecialWorkspaceToolsInstructions returns a shared prompt section that
// explains the workspace-level generation and analysis tools used by both chat
// agents and workflow-builder agents.
func GetSpecialWorkspaceToolsInstructions() string {
	return `## Special Workspace Tools

Use these tools when you need a direct provider-backed capability instead of general chat reasoning:

- ` + "`generate_text_llm(user_message, tier)`" + ` — Generate text with one direct LLM call using the workspace tier config. ` + "`tier`" + ` must be ` + "`high`" + `, ` + "`medium`" + `, or ` + "`low`" + `.
- ` + "`search_web_llm(query, provider?)`" + ` — Run a live web search using a published search-capable provider from ` + "`config/published-llms.json`" + `. Optional ` + "`provider`" + ` forces a specific published provider.
- ` + "`image_gen(prompt, output_path, provider?)`" + ` — Generate images using ` + "`config/image-generation-config.json`" + ` or an explicit provider override. ` + "`output_path`" + ` is required and must be a workspace-relative destination chosen by the caller.
- ` + "`image_edit(image_path, output_path, prompt, provider?)`" + ` — Edit an existing workspace image. ` + "`output_path`" + ` is required and must be a workspace-relative destination chosen by the caller.
- ` + "`generate_video(prompt, output_path, provider?)`" + ` — Generate videos using ` + "`config/video-generation-config.json`" + ` or an explicit provider override. ` + "`output_path`" + ` is required and must be a workspace-relative destination chosen by the caller.
- ` + "`read_image(filepath, query)`" + ` — Analyze an image file using ` + "`config/image-analysis-config.json`" + `. If no image-analysis config exists, it falls back to the current chat model.

Provider setup rules:
- Keep provider auth in ` + "`config/provider-api-keys.json`" + ` using the ` + "`set_provider_auth`" + ` tool. Do not hand-edit the encrypted auth file.
- Search provider routing comes from ` + "`config/published-llms.json`" + `.
- Image generation defaults come from ` + "`config/image-generation-config.json`" + `.
- Video generation defaults come from ` + "`config/video-generation-config.json`" + `.
- Image analysis defaults come from ` + "`config/image-analysis-config.json`" + `.`
}
