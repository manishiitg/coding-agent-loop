package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// webSearchTool gives the agent live web search so it can bring in current global
// best practices, exam/board patterns, and quality resources — the "coach who is
// one step ahead" capability. It uses Exa's keyless hosted MCP server, which
// returns rich, LLM-ready results (title, highlights, URL, date).
func webSearchTool() agentsession.Tool {
	return agentsession.Tool{
		Name:        "web_search",
		Description: "Search the web for current information — education best practices, learning-science techniques, exam/board patterns, quality resources. Returns the top results (title, highlights, URL). Use it to bring in global best practices the parent may not know, then translate them into concrete steps for this child.",
		Category:    "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "the search query"},
			},
			"required": []string{"query"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			q, _ := args["query"].(string)
			q = strings.TrimSpace(q)
			if q == "" {
				return "", fmt.Errorf("query is required")
			}
			if out, err := exaSearch(ctx, q); err == nil && strings.TrimSpace(out) != "" {
				return out, nil
			}
			return "(web search is unavailable right now — rely on your own knowledge of best practices)", nil
		},
	}
}

func mcpHeaders(req *http.Request, sessionID string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2024-11-05")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
}

// exaSearch calls Exa's keyless hosted MCP server (streamable HTTP): initialize
// to obtain a session, send the initialized notification, then tools/call
// web_search_exa. Responses are SSE (event/data lines).
func exaSearch(ctx context.Context, query string) (string, error) {
	const endpoint = "https://mcp.exa.ai/mcp"
	client := &http.Client{Timeout: 30 * time.Second}

	post := func(body string, sessionID string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		mcpHeaders(req, sessionID)
		return client.Do(req)
	}

	initResp, err := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"sparkquill","version":"1"}}}`, "")
	if err != nil {
		return "", err
	}
	sid := initResp.Header.Get("Mcp-Session-Id")
	_, _ = io.Copy(io.Discard, io.LimitReader(initResp.Body, 1<<20))
	initResp.Body.Close()
	if sid == "" {
		return "", fmt.Errorf("exa: no session id")
	}

	if nResp, nErr := post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid); nErr == nil {
		_, _ = io.Copy(io.Discard, nResp.Body)
		nResp.Body.Close()
	}

	callReq, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]interface{}{
			"name":      "web_search_exa",
			"arguments": map[string]interface{}{"query": query, "numResults": 5},
		},
	})
	callResp, err := post(string(callReq), sid)
	if err != nil {
		return "", err
	}
	defer callResp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(callResp.Body, 1<<20))
	return parseSSEResult(string(raw)), nil
}

// parseSSEResult extracts the tools/call result text from an SSE response body.
func parseSSEResult(body string) string {
	var dataLines []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	var parsed struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	for i := len(dataLines) - 1; i >= 0; i-- {
		if json.Unmarshal([]byte(dataLines[i]), &parsed) == nil && len(parsed.Result.Content) > 0 {
			var sb strings.Builder
			for _, c := range parsed.Result.Content {
				sb.WriteString(c.Text)
				sb.WriteString("\n")
			}
			return strings.TrimSpace(sb.String())
		}
	}
	return ""
}
