package server

import (
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/costledger"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

// crewRunTokenUsageFromSummary converts one ledger execution summary into
// the per-run usage shape stored on schedule runs. The per-model split is
// preserved so the calling workflow can attribute the same models in its
// own cost breakdown.
func crewRunTokenUsageFromSummary(summary *costledger.Summary) *workflowtypes.CrewRunTokenUsage {
	usage := &workflowtypes.CrewRunTokenUsage{}
	if summary == nil {
		return usage
	}
	for _, modelID := range summary.SortedModels() {
		aggregate := summary.ByModel[modelID]
		if aggregate == nil {
			continue
		}
		usage.AddModel(modelID, aggregate.Provider,
			aggregate.PromptTokens, aggregate.CompletionTokens, aggregate.ReasoningTokens,
			aggregate.CacheReadTokens, aggregate.CacheWriteTokens,
			aggregate.TotalCostUSD, aggregate.CallCount)
	}
	return usage
}

// accumulateAutomationTurnUsage folds one automation turn's ledger spend into
// the run total. A missing ledger, unknown query, or summarize failure is a
// silent no-op: cost recording must never fail the run it measures.
func accumulateAutomationTurnUsage(ledger *costledger.Ledger, total *workflowtypes.CrewRunTokenUsage, queryID string) {
	if ledger == nil || total == nil || queryID == "" {
		return
	}
	summary, err := ledger.SummarizeExecution(queryID)
	if err != nil || summary == nil {
		return
	}
	total.Merge(crewRunTokenUsageFromSummary(summary))
}
