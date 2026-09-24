package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/costledger"
)

// Consolidated cost view (Providers → Costs): spend across every workflow and
// Crew the caller can see, split by scope and model. The ledger keys spend by
// the raw workflow_id it was attributed to; this folds those into one row per
// workflow or Crew root and drops rows the caller may not open. Totals are
// recomputed from the visible rows only, so a member never sees spend they
// could not otherwise account for. Chat and unattributed spend is admin-only.

const (
	costOverviewKindWorkflow = "workflow"
	costOverviewKindCrew     = "crew"
	costOverviewKindOther    = "other"

	costOverviewOtherID = "other"
	crewProjectsSegment = "Chats/Work/projects/"
)

type costOverviewItem struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	OwnerID string `json:"owner_id,omitempty"`
	costledger.WorkflowAggregate
}

type costOverviewResponse struct {
	From       string                           `json:"from,omitempty"`
	To         string                           `json:"to,omitempty"`
	Total      costledger.Aggregate             `json:"total"`
	ByProvider map[string]*costledger.Aggregate `json:"by_provider"`
	ByModel    map[string]*costledger.Aggregate `json:"by_model"`
	Items      []*costOverviewItem              `json:"items"`
	// IncludesOther reports whether chat/unattributed spend is in the view
	// (admins only), so the UI can say what the total covers.
	IncludesOther bool `json:"includes_other"`
}

// costOverviewRoot folds a ledger workflow_id into the row it belongs to.
func costOverviewRoot(workflowID string) (id, kind, name, ownerID string) {
	path := strings.Trim(strings.ReplaceAll(strings.TrimSpace(workflowID), "\\", "/"), "/")
	if rest, ok := strings.CutPrefix(path, "Workflow/"); ok {
		folder, _, _ := strings.Cut(rest, "/")
		if folder != "" {
			return "Workflow/" + folder, costOverviewKindWorkflow, folder, ""
		}
	}
	if i := strings.Index(path, crewProjectsSegment); i >= 0 && (i == 0 || path[i-1] == '/') {
		project, _, _ := strings.Cut(path[i+len(crewProjectsSegment):], "/")
		if project != "" {
			root := path[:i+len(crewProjectsSegment)] + project
			owner, _ := crewProjectOwnerID(root)
			return root, costOverviewKindCrew, project, owner
		}
	}
	return costOverviewOtherID, costOverviewKindOther, "Chats & other", ""
}

func mergeWorkflowAggregate(target *costledger.WorkflowAggregate, source *costledger.WorkflowAggregate) {
	target.Aggregate.Merge(source.Aggregate)
	if target.ByScope == nil {
		target.ByScope = make(map[string]*costledger.Aggregate)
	}
	if target.ByModel == nil {
		target.ByModel = make(map[string]*costledger.Aggregate)
	}
	mergeAggregateMap(target.ByScope, source.ByScope)
	mergeAggregateMap(target.ByModel, source.ByModel)
}

func mergeAggregateMap(target, source map[string]*costledger.Aggregate) {
	for key, aggregate := range source {
		if aggregate == nil {
			continue
		}
		bucket, ok := target[key]
		if !ok {
			bucket = &costledger.Aggregate{}
			target[key] = bucket
		}
		bucket.Merge(*aggregate)
	}
}

// buildCostOverview folds and filters a ledger summary. visible decides
// whether a workflow or Crew row is shown; includeOther gates chat and
// unattributed spend.
func buildCostOverview(summary *costledger.Summary, visible func(id, kind string) bool, includeOther bool) *costOverviewResponse {
	resp := &costOverviewResponse{
		ByProvider:    make(map[string]*costledger.Aggregate),
		ByModel:       make(map[string]*costledger.Aggregate),
		Items:         []*costOverviewItem{},
		IncludesOther: includeOther,
	}
	if summary == nil {
		return resp
	}
	resp.From, resp.To = summary.From, summary.To
	items := make(map[string]*costOverviewItem)
	allowed := make(map[string]bool)
	for workflowID, aggregate := range summary.ByWorkflow {
		if aggregate == nil {
			continue
		}
		id, kind, name, ownerID := costOverviewRoot(workflowID)
		ok, seen := allowed[id]
		if !seen {
			ok = includeOther
			if kind != costOverviewKindOther {
				ok = visible(id, kind)
			}
			allowed[id] = ok
		}
		if !ok {
			continue
		}
		item, exists := items[id]
		if !exists {
			item = &costOverviewItem{ID: id, Kind: kind, Name: name, OwnerID: ownerID}
			items[id] = item
		}
		mergeWorkflowAggregate(&item.WorkflowAggregate, aggregate)
	}
	for _, item := range items {
		resp.Items = append(resp.Items, item)
		resp.Total.Merge(item.Aggregate)
		mergeAggregateMap(resp.ByModel, item.ByModel)
		for _, aggregate := range item.ByModel {
			provider := aggregate.Provider
			if provider == "" {
				provider = "unknown"
			}
			bucket, ok := resp.ByProvider[provider]
			if !ok {
				bucket = &costledger.Aggregate{}
				resp.ByProvider[provider] = bucket
			}
			bucket.Merge(*aggregate)
		}
	}
	sort.Slice(resp.Items, func(i, j int) bool {
		if resp.Items[i].TotalCostUSD != resp.Items[j].TotalCostUSD {
			return resp.Items[i].TotalCostUSD > resp.Items[j].TotalCostUSD
		}
		return resp.Items[i].ID < resp.Items[j].ID
	})
	return resp
}

// handleCostOverview is GET /api/cost/overview. Optional `from` and `to`
// (YYYY-MM-DD, UTC) bound the range, as for /api/cost/summary.
func (api *StreamingAPI) handleCostOverview(w http.ResponseWriter, r *http.Request) {
	if api.costLedger == nil {
		http.Error(w, `{"error":"cost ledger not initialized"}`, http.StatusServiceUnavailable)
		return
	}
	summary, err := api.costLedger.Summarize(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	visible := func(id, kind string) bool {
		if kind == costOverviewKindCrew {
			// Crew visibility is universal (see crew_directory.go).
			return true
		}
		return currentUserWorkflowAccess(r, id) != WorkflowAccessNone
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildCostOverview(summary, visible, currentUserIsAdmin(r)))
}
