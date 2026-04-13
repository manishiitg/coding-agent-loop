// Package costledger persists a global append-only LLM cost log through the
// workspace API and exposes a date-/model-aggregated summary view. There is no
// per-session state here — just one line per token_usage event across every
// agent run.
package costledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const ledgerWorkspacePath = "_system/costs.jsonl"

type workspaceAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

// Entry is a single cost record — one LLM call.
type Entry struct {
	Timestamp        time.Time `json:"ts"`
	SessionID        string    `json:"session_id,omitempty"`
	UserID           string    `json:"user_id,omitempty"`
	AgentMode        string    `json:"agent_mode,omitempty"`
	Component        string    `json:"component,omitempty"`
	CorrelationID    string    `json:"correlation_id,omitempty"`
	Provider         string    `json:"provider,omitempty"`
	ModelID          string    `json:"model_id,omitempty"`
	PromptTokens     int       `json:"prompt_tokens,omitempty"`
	CompletionTokens int       `json:"completion_tokens,omitempty"`
	ReasoningTokens  int       `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int       `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int       `json:"cache_write_tokens,omitempty"`
	TotalCostUSD     float64   `json:"total_cost_usd,omitempty"`
}

// Aggregate is the rolled-up token + cost total for a date/model bucket.
type Aggregate struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	CallCount        int     `json:"call_count"`
}

func (a *Aggregate) add(e Entry) {
	a.PromptTokens += e.PromptTokens
	a.CompletionTokens += e.CompletionTokens
	a.ReasoningTokens += e.ReasoningTokens
	a.CacheReadTokens += e.CacheReadTokens
	a.CacheWriteTokens += e.CacheWriteTokens
	a.TotalCostUSD += e.TotalCostUSD
	a.CallCount++
}

// Summary is the aggregated view returned by Summarize.
type Summary struct {
	From    string                `json:"from,omitempty"`
	To      string                `json:"to,omitempty"`
	Total   Aggregate             `json:"total"`
	ByDate  map[string]*Aggregate `json:"by_date"`  // YYYY-MM-DD UTC
	ByModel map[string]*Aggregate `json:"by_model"` // model_id
}

// Ledger appends token_usage records to _system/costs.jsonl through the
// workspace API and can produce aggregated summaries. Safe for concurrent
// appends within a single process via an internal mutex.
type Ledger struct {
	baseURL string
	client  *http.Client
	mu      sync.Mutex
}

// NewLedger creates a ledger that persists to _system/costs.jsonl via the
// workspace API.
func NewLedger(workspaceAPIURL string) *Ledger {
	return &Ledger{
		baseURL: strings.TrimRight(strings.TrimSpace(workspaceAPIURL), "/"),
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (l *Ledger) workspacePathURL(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return l.baseURL + "/api/documents/" + strings.Join(segments, "/")
}

func (l *Ledger) readFile(path string) ([]byte, bool, error) {
	req, err := http.NewRequest(http.MethodGet, l.workspacePathURL(path), nil)
	if err != nil {
		return nil, false, fmt.Errorf("costledger: create read request for %s: %w", path, err)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("costledger: read %s via workspace API: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("costledger: read response body for %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("costledger: workspace API returned status %d for %s: %s", resp.StatusCode, path, string(body))
	}

	var apiResp workspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, false, fmt.Errorf("costledger: parse workspace API response for %s: %w", path, err)
	}
	if strings.Contains(apiResp.Message, "File does not exist") || strings.Contains(apiResp.Error, "File not found") {
		return nil, false, nil
	}
	if !apiResp.Success {
		return nil, false, fmt.Errorf("costledger: workspace API error for %s: %s", path, apiResp.Error)
	}

	var data struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(apiResp.Data, &data); err != nil {
		return nil, false, fmt.Errorf("costledger: parse content for %s: %w", path, err)
	}
	return []byte(data.Content), true, nil
}

func (l *Ledger) writeFile(path string, content []byte) error {
	requestBody, err := json.Marshal(map[string]string{"content": string(content)})
	if err != nil {
		return fmt.Errorf("costledger: marshal content for %s: %w", path, err)
	}
	req, err := http.NewRequest(http.MethodPut, l.workspacePathURL(path), bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("costledger: create write request for %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("costledger: write %s via workspace API: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("costledger: workspace API returned status %d for %s: %s", resp.StatusCode, path, string(body))
	}
	return nil
}

// Append writes one entry as a JSONL line. Missing Timestamp is filled in.
func (l *Ledger) Append(e Entry) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("costledger: marshal entry: %w", err)
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	content, exists, err := l.readFile(ledgerWorkspacePath)
	if err != nil {
		return err
	}
	if exists && len(content) > 0 {
		line = append(content, line...)
	}
	return l.writeFile(ledgerWorkspacePath, line)
}

// Summarize scans the ledger and rolls entries up by date and model. from/to
// are inclusive date bounds in YYYY-MM-DD (UTC); empty strings mean unbounded.
func (l *Ledger) Summarize(from, to string) (*Summary, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	summary := &Summary{
		From:    from,
		To:      to,
		ByDate:  make(map[string]*Aggregate),
		ByModel: make(map[string]*Aggregate),
	}

	content, exists, err := l.readFile(ledgerWorkspacePath)
	if err != nil {
		return nil, err
	}
	if !exists || len(content) == 0 {
		return summary, nil
	}

	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		date := e.Timestamp.UTC().Format("2006-01-02")
		if from != "" && date < from {
			continue
		}
		if to != "" && date > to {
			continue
		}
		summary.Total.add(e)
		bucket, ok := summary.ByDate[date]
		if !ok {
			bucket = &Aggregate{}
			summary.ByDate[date] = bucket
		}
		bucket.add(e)
		if e.ModelID != "" {
			mb, ok := summary.ByModel[e.ModelID]
			if !ok {
				mb = &Aggregate{}
				summary.ByModel[e.ModelID] = mb
			}
			mb.add(e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("costledger: scan %s: %w", ledgerWorkspacePath, err)
	}
	return summary, nil
}

// SortedDates returns the date keys from a summary in ascending order.
// Kept here so handlers don't have to re-implement the sort.
func (s *Summary) SortedDates() []string {
	out := make([]string, 0, len(s.ByDate))
	for d := range s.ByDate {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// SortedModels returns model keys sorted by total cost descending.
func (s *Summary) SortedModels() []string {
	out := make([]string, 0, len(s.ByModel))
	for m := range s.ByModel {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return s.ByModel[out[i]].TotalCostUSD > s.ByModel[out[j]].TotalCostUSD
	})
	return out
}
