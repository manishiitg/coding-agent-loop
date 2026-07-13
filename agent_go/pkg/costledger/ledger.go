// Package costledger persists immutable LLM and paid-tool cost events and
// exposes date/model aggregates. SQLite is the authoritative production store;
// the workspace-API JSONL implementation remains only for migration-era tests.
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
	EventID        string    `json:"event_id,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Timestamp      time.Time `json:"ts"`
	SessionID      string    `json:"session_id,omitempty"`
	UserID         string    `json:"user_id,omitempty"`
	WorkflowID     string    `json:"workflow_id,omitempty"`
	RunID          string    `json:"run_id,omitempty"`
	ExecutionID    string    `json:"execution_id,omitempty"`
	Scope          string    `json:"scope,omitempty"`
	AgentMode      string    `json:"agent_mode,omitempty"`
	Component      string    `json:"component,omitempty"`
	CorrelationID  string    `json:"correlation_id,omitempty"`
	Provider       string    `json:"provider,omitempty"`
	ModelID        string    `json:"model_id,omitempty"`
	// EffectiveModelID is the model the CLI/provider ACTUALLY served
	// the turn with — may drift from ModelID when the user picked an
	// alias like "auto" or "cursor-cli", or when a /model swap happened
	// mid-session. Empty when the provider doesn't surface it.
	EffectiveProvider string  `json:"effective_provider,omitempty"`
	EffectiveModelID  string  `json:"effective_model_id,omitempty"`
	TurnCount         int     `json:"turn_count,omitempty"`
	LLMCallCount      int     `json:"llm_call_count,omitempty"`
	PromptTokens      int     `json:"prompt_tokens,omitempty"`
	CompletionTokens  int     `json:"completion_tokens,omitempty"`
	ReasoningTokens   int     `json:"reasoning_tokens,omitempty"`
	CacheReadTokens   int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens  int     `json:"cache_write_tokens,omitempty"`
	TotalCostUSD      float64 `json:"total_cost_usd,omitempty"`
	// CostUSDSource flags whether TotalCostUSD came from the provider
	// ("provider", e.g. claude's total_cost_usd) or was computed
	// downstream from tokens × registry rates ("estimated"). For
	// subscription-billed CLIs (Cursor, Codex Pro) "estimated" is a
	// SHADOW cost — what the same workload would cost via the
	// underlying per-token API, NOT the flat-plan bill.
	CostUSDSource     string                 `json:"cost_usd_source,omitempty"`
	Currency          string                 `json:"currency,omitempty"`
	BillingBasis      string                 `json:"billing_basis,omitempty"`
	PricingSource     string                 `json:"pricing_source,omitempty"`
	PricingVersion    string                 `json:"pricing_version,omitempty"`
	ToolName          string                 `json:"tool_name,omitempty"`
	OperationMetadata map[string]interface{} `json:"operation_metadata,omitempty"`
}

// Aggregate is the rolled-up token + cost total for a date/model bucket.
type Aggregate struct {
	PromptTokens          int     `json:"prompt_tokens"`
	CompletionTokens      int     `json:"completion_tokens"`
	ReasoningTokens       int     `json:"reasoning_tokens"`
	CacheReadTokens       int     `json:"cache_read_tokens"`
	CacheWriteTokens      int     `json:"cache_write_tokens"`
	TotalCostUSD          float64 `json:"total_cost_usd"`
	CallCount             int     `json:"call_count"`
	AccountingEventCount  int     `json:"accounting_event_count"`
	UnpricedCallCount     int     `json:"unpriced_call_count"`
	ProviderActualCostUSD float64 `json:"provider_actual_cost_usd"`
	TokenEstimateCostUSD  float64 `json:"token_estimate_cost_usd"`
	SubscriptionShadowUSD float64 `json:"subscription_shadow_cost_usd"`
}

func (a *Aggregate) add(e Entry) {
	a.PromptTokens += e.PromptTokens
	a.CompletionTokens += e.CompletionTokens
	a.ReasoningTokens += e.ReasoningTokens
	a.CacheReadTokens += e.CacheReadTokens
	a.CacheWriteTokens += e.CacheWriteTokens
	a.TotalCostUSD += e.TotalCostUSD
	a.CallCount += e.LLMCallCount
	a.AccountingEventCount++
	if e.LLMCallCount > 0 && e.BillingBasis == "unpriced" {
		a.UnpricedCallCount += e.LLMCallCount
	}
	switch e.BillingBasis {
	case "provider_actual":
		a.ProviderActualCostUSD += e.TotalCostUSD
	case "subscription_shadow":
		a.SubscriptionShadowUSD += e.TotalCostUSD
	case "token_estimate":
		a.TokenEstimateCostUSD += e.TotalCostUSD
	}
}

// DateAggregate is one row in the per-date rollup. It embeds Aggregate
// so its JSON shape stays flat (existing consumers reading
// `prompt_tokens`/`call_count`/etc. at the date level keep working),
// and adds a per-model breakdown for clients that want to expand the
// row.
type DateAggregate struct {
	Aggregate
	ByModel map[string]*Aggregate `json:"by_model,omitempty"`
}

// Summary is the aggregated view returned by Summarize.
type Summary struct {
	From     string                    `json:"from,omitempty"`
	To       string                    `json:"to,omitempty"`
	Total    Aggregate                 `json:"total"`
	ByDate   map[string]*DateAggregate `json:"by_date"`  // YYYY-MM-DD UTC
	ByModel  map[string]*Aggregate     `json:"by_model"` // model_id
	Coverage Coverage                  `json:"coverage"`
}

// Coverage reports whether the aggregate omitted or could not price evidence.
type Coverage struct {
	Source                string `json:"source"`
	MalformedEventCount   int    `json:"malformed_event_count"`
	QuarantinedEventCount int    `json:"quarantined_event_count"`
}

// Ledger writes immutable cost events and produces aggregate summaries. The
// SQLite implementation is safe across independent Ledger instances and uses
// idempotency keys to make retries harmless.
type Ledger struct {
	baseURL string
	client  *http.Client
	db      sqliteStore
	mu      sync.Mutex
}

var (
	defaultLedgerMu sync.RWMutex
	defaultLedger   *Ledger
)

// SetDefaultLedger publishes the process-wide production ledger used by paid
// virtual tools. The server owns its lifecycle; callers must not close it.
func SetDefaultLedger(ledger *Ledger) {
	defaultLedgerMu.Lock()
	defaultLedger = ledger
	defaultLedgerMu.Unlock()
}

// DefaultLedger returns the process-wide ledger, when the server initialized
// one. Tests and standalone tools may leave it unset and open their own store.
func DefaultLedger() *Ledger {
	defaultLedgerMu.RLock()
	defer defaultLedgerMu.RUnlock()
	return defaultLedger
}

type sqliteStore interface {
	append(Entry) error
	summarize(from, to string) (*Summary, error)
	migrateLegacyJSONL(path string) (MigrationReport, error)
	close() error
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
	if l == nil {
		return fmt.Errorf("costledger: nil ledger")
	}
	if l.db != nil {
		return l.db.append(e)
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	normalizeEntry(&e)
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
	if l == nil {
		return nil, fmt.Errorf("costledger: nil ledger")
	}
	if l.db != nil {
		return l.db.summarize(from, to)
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	summary := &Summary{
		From:     from,
		To:       to,
		ByDate:   make(map[string]*DateAggregate),
		ByModel:  make(map[string]*Aggregate),
		Coverage: Coverage{Source: "legacy_jsonl"},
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
			summary.Coverage.MalformedEventCount++
			continue
		}
		normalizeEntry(&e)
		date := e.Timestamp.UTC().Format("2006-01-02")
		if from != "" && date < from {
			continue
		}
		if to != "" && date > to {
			continue
		}
		addEntryToSummary(summary, date, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("costledger: scan %s: %w", ledgerWorkspacePath, err)
	}
	return summary, nil
}

// Close releases the SQLite connection. It is a no-op for the legacy API ledger.
func (l *Ledger) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.close()
}

// MigrateLegacyJSONL imports valid rows idempotently and quarantines malformed
// rows. It is supported only by the SQLite ledger.
func (l *Ledger) MigrateLegacyJSONL(path string) (MigrationReport, error) {
	if l == nil || l.db == nil {
		return MigrationReport{}, fmt.Errorf("costledger: legacy migration requires SQLite ledger")
	}
	return l.db.migrateLegacyJSONL(path)
}

func addEntryToSummary(summary *Summary, date string, e Entry) {
	summary.Total.add(e)
	bucket, ok := summary.ByDate[date]
	if !ok {
		bucket = &DateAggregate{ByModel: make(map[string]*Aggregate)}
		summary.ByDate[date] = bucket
	}
	bucket.Aggregate.add(e)
	modelID := e.EffectiveModelID
	if modelID == "" {
		modelID = e.ModelID
	}
	if modelID == "" {
		return
	}
	dm, ok := bucket.ByModel[modelID]
	if !ok {
		dm = &Aggregate{}
		bucket.ByModel[modelID] = dm
	}
	dm.add(e)
	mb, ok := summary.ByModel[modelID]
	if !ok {
		mb = &Aggregate{}
		summary.ByModel[modelID] = mb
	}
	mb.add(e)
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
