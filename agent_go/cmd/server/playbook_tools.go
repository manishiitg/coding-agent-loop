package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type playbookSearchResult struct {
	ID                   string                   `json:"id"`
	Title                string                   `json:"title"`
	Category             string                   `json:"category"`
	Description          string                   `json:"description"`
	Version              string                   `json:"version"`
	SetupAreas           []string                 `json:"setup_areas"`
	RequiredCapabilities []string                 `json:"required_capabilities"`
	Outputs              []string                 `json:"outputs"`
	RecommendedTools     []map[string]interface{} `json:"recommended_tools,omitempty"`
	Installed            bool                     `json:"installed"`
	InstalledStatus      string                   `json:"installed_status,omitempty"`
	Score                int                      `json:"-"`
}

func searchPlaybooks(items []playbookCatalogItem, installed []InstalledPlaybook, query string, limit int) []playbookSearchResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	if limit < 1 || limit > 10 {
		limit = 6
	}
	installedByID := make(map[string]InstalledPlaybook, len(installed))
	for _, item := range installed {
		installedByID[item.ID] = item
	}
	tokens := strings.Fields(query)
	results := make([]playbookSearchResult, 0)
	for _, item := range items {
		setupAreas := make([]string, 0, len(item.SetupInputs))
		var searchable []string
		searchable = append(searchable, item.ID, item.Title, item.Category, item.Description)
		for _, input := range item.SetupInputs {
			if label, _ := input["label"].(string); strings.TrimSpace(label) != "" {
				setupAreas = append(setupAreas, label)
				searchable = append(searchable, label)
			}
		}
		searchable = append(searchable, item.RequiredCapabilities...)
		searchable = append(searchable, item.Outputs...)
		for _, tool := range item.RecommendedTools {
			for _, key := range []string{"name", "purpose", "capability", "type"} {
				if value, _ := tool[key].(string); value != "" {
					searchable = append(searchable, value)
				}
			}
		}
		corpus := strings.ToLower(strings.Join(searchable, " "))
		score := 0
		if strings.Contains(strings.ToLower(item.Title), query) || strings.Contains(strings.ToLower(item.ID), query) {
			score += 20
		} else if strings.Contains(corpus, query) {
			score += 10
		}
		for _, token := range tokens {
			if strings.Contains(strings.ToLower(item.Title), token) {
				score += 4
			} else if strings.Contains(corpus, token) {
				score += 2
			}
		}
		if score == 0 {
			continue
		}
		installedItem, isInstalled := installedByID[item.ID]
		results = append(results, playbookSearchResult{
			ID: item.ID, Title: item.Title, Category: item.Category, Description: item.Description,
			Version: item.Version, SetupAreas: setupAreas, RequiredCapabilities: item.RequiredCapabilities,
			Outputs: item.Outputs, RecommendedTools: item.RecommendedTools, Installed: isInstalled,
			InstalledStatus: installedItem.Status, Score: score,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Category != results[j].Category {
			return results[i].Category < results[j].Category
		}
		return results[i].Title < results[j].Title
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func (api *StreamingAPI) registerPlaybookSearchTool(registrar definitionToolRegistrar, workspacePath string) error {
	description := "Search the AgentWorks playbook catalog by the user's intent, operating area, capability, tool, or expected outcome. Use this before improvising a generic workflow setup when a reusable engineering playbook may fit. Results include setup areas, deliverables, recommended tools, and whether the playbook is already installed in this workflow. Recommend only relevant matches and explain the fit. This tool never installs anything: ask the user to install the chosen playbook, and use open_workspace_view(view=\"playbooks\") to show the catalog when available."
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "Plain-language workflow need, capability, area, tool, or desired outcome."},
			"limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 10, "default": 6},
		},
		"required": []string{"query"},
	}
	return registrar.RegisterCustomTool("search_playbooks", description, params, func(ctx context.Context, args map[string]interface{}) (string, error) {
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return "", fmt.Errorf("query is required")
		}
		limit := 6
		switch value := args["limit"].(type) {
		case float64:
			limit = int(value)
		case int:
			limit = value
		}
		items, err := loadPlaybookCatalog()
		if err != nil {
			return "", err
		}
		var installed []InstalledPlaybook
		if strings.TrimSpace(workspacePath) != "" {
			if manifest, found, readErr := ReadWorkflowManifest(ctx, workspacePath); readErr == nil && found {
				installed = manifest.InstalledPlaybooks
			}
		}
		matches := searchPlaybooks(items, installed, query, limit)
		payload := map[string]interface{}{
			"query": query, "matches": matches, "total": len(matches),
			"next_action": "Recommend the best-fitting playbook with reasons. If it is not installed, ask the user to install it and open the Playbooks view. Do not claim installation or mutate the workflow.",
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}, "playbook_tools")
}
