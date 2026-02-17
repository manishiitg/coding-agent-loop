package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcp-agent-builder-go/agent_go/pkg/skills"
	"mcp-agent-builder-go/agent_go/pkg/subagents"
)

// AnalysisResult contains the output of the bot analysis LLM call
type AnalysisResult struct {
	Summary           string   `json:"summary"`
	RequiredServers   []string `json:"required_servers"`
	RequiredSkills    []string `json:"required_skills"`
	RequiredSubAgents []string `json:"required_subagents"`
	RequiredSecrets   []string `json:"required_secrets"`
	DelegationMode    string   `json:"delegation_mode"`  // "plan" or "off"
	NeedsWorkspace    bool     `json:"needs_workspace"`
	NeedsBrowser      bool     `json:"needs_browser"`
	MatchedPresetID   string   `json:"matched_preset_id"`
	MatchedPresetName string   `json:"matched_preset_name"`
	RewrittenQuery    string   `json:"rewritten_query"`
	Confidence        float64  `json:"confidence"`
}

// BotAnalyzer performs the lightweight LLM call to determine what capabilities are needed
type BotAnalyzer struct {
	mcpConfigPath string
	workspaceURL  string
}

// NewBotAnalyzer creates a new analyzer
func NewBotAnalyzer(mcpConfigPath, workspaceURL string) *BotAnalyzer {
	return &BotAnalyzer{
		mcpConfigPath: mcpConfigPath,
		workspaceURL:  workspaceURL,
	}
}

// Analyze runs the analysis LLM call to determine needed capabilities
func (a *BotAnalyzer) Analyze(ctx context.Context, query string, threadHistory []ThreadMessage) (*AnalysisResult, error) {
	// Gather available capabilities
	capabilities := a.gatherCapabilities()

	// Build the analysis prompt
	prompt := a.buildAnalysisPrompt(query, threadHistory, capabilities)

	// Get provider/model from env or use defaults
	provider := os.Getenv("BOT_ANALYSIS_PROVIDER")
	if provider == "" {
		provider = "anthropic"
	}
	model := os.Getenv("BOT_ANALYSIS_MODEL")
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}

	log.Printf("[BOT_ANALYSIS] Running analysis with %s/%s for query: %s", provider, model, botTruncate(query, 80))

	// Initialize LLM
	llm, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.Provider(provider),
		ModelID:     model,
		Temperature: 0.0,
		Context:     ctx,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize analysis LLM: %w", err)
	}

	// Make the LLM call
	messages := []llmtypes.MessageContent{
		llmtypes.TextPart(llmtypes.ChatMessageTypeSystem, prompt),
		llmtypes.TextPart(llmtypes.ChatMessageTypeHuman, query),
	}

	resp, err := llm.GenerateContent(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("analysis LLM call failed: %w", err)
	}

	// Extract text from response
	responseText := ""
	for _, choice := range resp.Choices {
		if choice.Content != "" {
			responseText += choice.Content
		}
	}

	// Parse the JSON response
	result, err := a.parseAnalysisResponse(responseText)
	if err != nil {
		log.Printf("[BOT_ANALYSIS] Failed to parse response, using defaults: %v", err)
		// Return a safe default
		return &AnalysisResult{
			Summary:        "I'll help you with that request",
			DelegationMode: "off",
			NeedsWorkspace: true,
			RewrittenQuery: query,
			Confidence:     0.3,
		}, nil
	}

	log.Printf("[BOT_ANALYSIS] Analysis complete: servers=%v skills=%v mode=%s confidence=%.2f",
		result.RequiredServers, result.RequiredSkills, result.DelegationMode, result.Confidence)

	return result, nil
}

// capabilityInfo holds available capabilities for the analysis prompt
type capabilityInfo struct {
	MCPServers []serverInfo `json:"mcp_servers"`
	Skills     []skillInfo  `json:"skills"`
	SubAgents  []agentInfo  `json:"subagents"`
	Presets    []presetInfo `json:"presets"`
}

type serverInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type skillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type agentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type presetInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Query string `json:"query,omitempty"`
	Mode  string `json:"mode,omitempty"`
}

// gatherCapabilities collects all available servers, skills, sub-agents, and presets
func (a *BotAnalyzer) gatherCapabilities() capabilityInfo {
	caps := capabilityInfo{}

	// Discover skills
	if a.workspaceURL != "" {
		discoveredSkills, err := skills.DiscoverSkills(a.workspaceURL)
		if err == nil {
			for _, s := range discoveredSkills {
				caps.Skills = append(caps.Skills, skillInfo{
					Name:        s.FolderName,
					Description: s.Frontmatter.Description,
				})
			}
		}

		// Discover sub-agents
		discoveredAgents, err := subagents.DiscoverSubAgents(a.workspaceURL)
		if err == nil {
			for _, sa := range discoveredAgents {
				caps.SubAgents = append(caps.SubAgents, agentInfo{
					Name:        sa.FolderName,
					Description: sa.Frontmatter.Description,
				})
			}
		}
	}

	// MCP servers from config file — we list known server names
	// The actual server descriptions would come from mcpConfig, but we keep it simple
	// The server layer can enhance this with full server metadata
	if a.mcpConfigPath != "" {
		// Parse MCP config for server names
		caps.MCPServers = a.loadMCPServerNames()
	}

	return caps
}

// loadMCPServerNames reads server names from the MCP config file
func (a *BotAnalyzer) loadMCPServerNames() []serverInfo {
	data, err := os.ReadFile(a.mcpConfigPath)
	if err != nil {
		return nil
	}

	var config struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil
	}

	var servers []serverInfo
	for name := range config.MCPServers {
		servers = append(servers, serverInfo{Name: name})
	}
	return servers
}

// buildAnalysisPrompt creates the system prompt for the analysis LLM call
func (a *BotAnalyzer) buildAnalysisPrompt(query string, threadHistory []ThreadMessage, caps capabilityInfo) string {
	var sb strings.Builder

	sb.WriteString(`You are an AI assistant router. Analyze the user's request and determine what capabilities are needed to fulfill it.

You MUST respond with ONLY a valid JSON object (no markdown, no explanation). The JSON schema is:

{
  "summary": "Brief description of what you'll do (1 sentence)",
  "required_servers": ["server_name1", "server_name2"],
  "required_skills": ["skill_name1"],
  "required_subagents": ["agent_name1"],
  "required_secrets": [],
  "delegation_mode": "off",
  "needs_workspace": false,
  "needs_browser": false,
  "matched_preset_id": "",
  "matched_preset_name": "",
  "rewritten_query": "The user's request, cleaned up and incorporating any thread context",
  "confidence": 0.8
}

Rules:
- delegation_mode: "plan" for complex multi-step tasks needing multiple agents, "off" for simple single-agent tasks
- needs_workspace: true if the task involves reading/writing files
- needs_browser: true if the task involves web browsing/scraping
- Only include servers/skills/subagents that are actually available (listed below)
- confidence: 0.0-1.0 indicating how confident you are in your analysis
- rewritten_query: incorporate thread context if provided, clean up the query

`)

	// Add available capabilities
	sb.WriteString("## Available MCP Servers\n")
	if len(caps.MCPServers) > 0 {
		for _, s := range caps.MCPServers {
			sb.WriteString(fmt.Sprintf("- %s", s.Name))
			if s.Description != "" {
				sb.WriteString(fmt.Sprintf(": %s", s.Description))
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("(none configured)\n")
	}

	sb.WriteString("\n## Available Skills\n")
	if len(caps.Skills) > 0 {
		for _, s := range caps.Skills {
			sb.WriteString(fmt.Sprintf("- %s", s.Name))
			if s.Description != "" {
				sb.WriteString(fmt.Sprintf(": %s", s.Description))
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("(none configured)\n")
	}

	sb.WriteString("\n## Available Sub-Agent Templates\n")
	if len(caps.SubAgents) > 0 {
		for _, sa := range caps.SubAgents {
			sb.WriteString(fmt.Sprintf("- %s", sa.Name))
			if sa.Description != "" {
				sb.WriteString(fmt.Sprintf(": %s", sa.Description))
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("(none configured)\n")
	}

	sb.WriteString("\n## Available Presets\n")
	if len(caps.Presets) > 0 {
		for _, p := range caps.Presets {
			sb.WriteString(fmt.Sprintf("- %s (id: %s, mode: %s)\n", p.Label, p.ID, p.Mode))
		}
	} else {
		sb.WriteString("(none configured)\n")
	}

	// Add thread context if available
	if len(threadHistory) > 0 {
		sb.WriteString("\n## Thread Context (prior conversation)\n")
		sb.WriteString("The user tagged the bot in an existing conversation. Here is the thread history:\n\n")
		for _, msg := range threadHistory {
			role := msg.UserName
			if msg.IsBot {
				role = "[bot]"
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n", role, msg.Text))
		}
		sb.WriteString("\nIncorporate this context into your rewritten_query.\n")
	}

	return sb.String()
}

// parseAnalysisResponse extracts the AnalysisResult from the LLM response
func (a *BotAnalyzer) parseAnalysisResponse(response string) (*AnalysisResult, error) {
	// Try to extract JSON from the response
	response = strings.TrimSpace(response)

	// Handle markdown code blocks
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock {
				jsonLines = append(jsonLines, line)
			}
		}
		response = strings.Join(jsonLines, "\n")
	}

	var result AnalysisResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("failed to parse analysis JSON: %w (response: %s)", err, botTruncate(response, 200))
	}

	// Validate and set defaults
	if result.DelegationMode == "" {
		result.DelegationMode = "off"
	}
	if result.Confidence == 0 {
		result.Confidence = 0.5
	}

	return &result, nil
}
