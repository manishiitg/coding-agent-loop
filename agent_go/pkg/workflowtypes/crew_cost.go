package workflowtypes

// CrewRunCaller stamps the workflow step that invoked a Crew run through a
// platform-internal trigger. It is recorded on the Crew run so Crew-side
// cost history shows what each caller spent.
type CrewRunCaller struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id,omitempty"`
	StepID     string `json:"step_id,omitempty"`
}

// CrewRunModelUsage is one model's share of a Crew run's token spend, summed
// from the cost ledger. CostUSD is the ledger's own total (provider actuals
// where the provider reports them); consumers that reprice from a rate card
// recompute from the token counts instead.
type CrewRunModelUsage struct {
	Provider         string  `json:"provider,omitempty"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	ReasoningTokens  int     `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	LLMCallCount     int     `json:"llm_call_count,omitempty"`
}

// CrewRunTokenUsage is a Crew run's whole token spend: exact totals plus the
// per-model split. Totals always equal the ByModel sum; both are stored so
// readers never have to re-derive one from the other.
type CrewRunTokenUsage struct {
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	ReasoningTokens  int     `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	LLMCallCount     int     `json:"llm_call_count,omitempty"`

	ByModel map[string]*CrewRunModelUsage `json:"by_model,omitempty"`
}

// Empty reports whether the usage carries no spend at all.
func (u *CrewRunTokenUsage) Empty() bool {
	if u == nil {
		return true
	}
	return u.PromptTokens == 0 && u.CompletionTokens == 0 && u.ReasoningTokens == 0 &&
		u.CacheReadTokens == 0 && u.CacheWriteTokens == 0 && u.CostUSD == 0 && u.LLMCallCount == 0
}

// AddModel folds one model's delta into the totals and the per-model split.
func (u *CrewRunTokenUsage) AddModel(modelID, provider string, prompt, completion, reasoning, cacheRead, cacheWrite int, costUSD float64, calls int) {
	if modelID == "" {
		modelID = "unknown"
	}
	u.PromptTokens += prompt
	u.CompletionTokens += completion
	u.ReasoningTokens += reasoning
	u.CacheReadTokens += cacheRead
	u.CacheWriteTokens += cacheWrite
	u.CostUSD += costUSD
	u.LLMCallCount += calls
	if u.ByModel == nil {
		u.ByModel = make(map[string]*CrewRunModelUsage)
	}
	entry := u.ByModel[modelID]
	if entry == nil {
		entry = &CrewRunModelUsage{}
		u.ByModel[modelID] = entry
	}
	if entry.Provider == "" {
		entry.Provider = provider
	}
	entry.PromptTokens += prompt
	entry.CompletionTokens += completion
	entry.ReasoningTokens += reasoning
	entry.CacheReadTokens += cacheRead
	entry.CacheWriteTokens += cacheWrite
	entry.CostUSD += costUSD
	entry.LLMCallCount += calls
}

// Merge folds another usage record into this one, model by model.
func (u *CrewRunTokenUsage) Merge(other *CrewRunTokenUsage) {
	if other == nil {
		return
	}
	if len(other.ByModel) == 0 {
		u.AddModel("unknown", "", other.PromptTokens, other.CompletionTokens, other.ReasoningTokens,
			other.CacheReadTokens, other.CacheWriteTokens, other.CostUSD, other.LLMCallCount)
		return
	}
	for modelID, usage := range other.ByModel {
		if usage == nil {
			continue
		}
		u.AddModel(modelID, usage.Provider, usage.PromptTokens, usage.CompletionTokens, usage.ReasoningTokens,
			usage.CacheReadTokens, usage.CacheWriteTokens, usage.CostUSD, usage.LLMCallCount)
	}
}
