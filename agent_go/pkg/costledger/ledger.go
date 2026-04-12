// Package costledger persists a global append-only LLM cost log to disk and
// exposes a date-/model-aggregated summary view. There is no per-session
// state here — just one line per token_usage event across every agent run.
package costledger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

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

// Ledger appends token_usage records to _system/costs.jsonl and can produce
// aggregated summaries. Safe for concurrent appends via an internal mutex.
type Ledger struct {
	path string
	mu   sync.Mutex
}

// NewLedger creates a ledger rooted at <workspaceDocsRoot>/_system/costs.jsonl.
// The _system directory is created on first use.
func NewLedger(workspaceDocsRoot string) *Ledger {
	return &Ledger{
		path: filepath.Join(workspaceDocsRoot, "_system", "costs.jsonl"),
	}
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
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("costledger: open %s: %w", l.path, err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("costledger: append to %s: %w", l.path, err)
	}
	return nil
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

	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return summary, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
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
		return nil, fmt.Errorf("costledger: scan %s: %w", l.path, err)
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
