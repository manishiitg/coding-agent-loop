package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

type workflowCostsResponse struct {
	Success         bool                              `json:"success"`
	PhaseTokenUsage *orchestrator.PhaseTokenUsageFile `json:"phase_token_usage,omitempty"`
	PhaseDailyCosts []workflowPhaseDailyCostEntry     `json:"phase_daily_costs"`
	Runs            []workflowRunCostEntry            `json:"runs"`
}

type StepOutputContent struct {
	FilePath string      `json:"file_path"`
	Content  interface{} `json:"content"`
	IsJSON   bool        `json:"is_json"`
}

type EvaluationStepScore struct {
	StepID        string             `json:"step_id"`
	Score         int                `json:"score"`
	MaxScore      int                `json:"max_score"`
	Reasoning     string             `json:"reasoning"`
	Evidence      string             `json:"evidence"`
	ContextOutput string             `json:"context_output,omitempty"`
	OutputContent *StepOutputContent `json:"output_content,omitempty"`
}

type EvaluationReport struct {
	TargetRunFolder  string                `json:"target_run_folder"`
	GeneratedAt      string                `json:"generated_at"`
	TotalScore       int                   `json:"total_score"`
	MaxPossibleScore int                   `json:"max_possible_score"`
	ScorePercentage  float64               `json:"score_percentage"`
	StepScores       []EvaluationStepScore `json:"step_scores"`
}

type EvaluationReportEntry struct {
	RunFolder string           `json:"run_folder"`
	Report    EvaluationReport `json:"report"`
}

type EvaluationAggregate struct {
	TotalRuns         int     `json:"total_runs"`
	AverageScore      float64 `json:"average_score"`
	AveragePercentage float64 `json:"average_percentage"`
	HighestScore      int     `json:"highest_score"`
	LowestScore       int     `json:"lowest_score"`
	MaxPossibleScore  int     `json:"max_possible_score"`
}

type workflowEvaluationReportsResponse struct {
	Success        bool                    `json:"success"`
	Reports        []EvaluationReportEntry `json:"reports"`
	Aggregate      *EvaluationAggregate    `json:"aggregate,omitempty"`
	EvaluationPlan *string                 `json:"evaluation_plan,omitempty"`
	Error          string                  `json:"error,omitempty"`
}

type workflowReviewDataResponse struct {
	Success     bool                              `json:"success"`
	Costs       workflowCostsResponse             `json:"costs"`
	Evaluations workflowEvaluationReportsResponse `json:"evaluations"`
}

func loadWorkflowCosts(ctx context.Context, workspacePath string) workflowCostsResponse {
	var phaseTokenUsage *orchestrator.PhaseTokenUsageFile
	if phaseUsage, err := readPhaseTokenUsageFromCosts(ctx, workspacePath); err == nil {
		phaseTokenUsage = phaseUsage
	}

	phaseDailyCosts, err := readAllPhaseTokenUsageFromCosts(ctx, workspacePath)
	if err != nil {
		phaseDailyCosts = []workflowPhaseDailyCostEntry{}
	}

	executionCosts, err := readAllRunTokenUsageFromCosts(ctx, workspacePath, orchestrator.CostScopeExecution)
	if err != nil {
		executionCosts = map[string]*orchestrator.TokenUsageFile{}
	}

	evaluationCosts, err := readAllRunTokenUsageFromCosts(ctx, workspacePath, orchestrator.CostScopeEvaluation)
	if err != nil {
		evaluationCosts = map[string]*orchestrator.TokenUsageFile{}
	}

	return workflowCostsResponse{
		Success:         true,
		PhaseTokenUsage: phaseTokenUsage,
		PhaseDailyCosts: phaseDailyCosts,
		Runs:            buildWorkflowRunCostEntries(executionCosts, evaluationCosts),
	}
}

func loadWorkflowEvaluationReports(ctx context.Context, workspacePath, runFolder string) workflowEvaluationReportsResponse {
	evaluationPlan := readWorkflowEvaluationPlan(ctx, workspacePath)
	reportMap, err := readAllEvaluationReportsFromScores(ctx, workspacePath)
	if err != nil {
		return workflowEvaluationReportsResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to read evaluation scores: %v", err),
		}
	}

	var reports []EvaluationReportEntry
	for runFolderName, report := range reportMap {
		if !workflowRunFolderMatches(runFolderName, runFolder) {
			continue
		}
		reports = append(reports, EvaluationReportEntry{
			RunFolder: runFolderName,
			Report:    report,
		})
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Report.GeneratedAt > reports[j].Report.GeneratedAt
	})

	return workflowEvaluationReportsResponse{
		Success:        true,
		Reports:        reports,
		Aggregate:      buildEvaluationAggregate(reports),
		EvaluationPlan: evaluationPlan,
	}
}

func workflowRunFolderMatches(candidate, requested string) bool {
	if strings.TrimSpace(requested) == "" {
		return true
	}
	return candidate == requested ||
		strings.HasPrefix(candidate, requested+"/") ||
		strings.HasPrefix(requested, candidate+"/")
}

func buildEvaluationAggregate(reports []EvaluationReportEntry) *EvaluationAggregate {
	if len(reports) == 0 {
		return nil
	}

	totalScore := 0
	totalPercentage := 0.0
	highestScore := 0
	lowestScore := reports[0].Report.TotalScore
	maxPossible := 0

	for _, entry := range reports {
		totalScore += entry.Report.TotalScore
		totalPercentage += entry.Report.ScorePercentage

		if entry.Report.TotalScore > highestScore {
			highestScore = entry.Report.TotalScore
		}
		if entry.Report.TotalScore < lowestScore {
			lowestScore = entry.Report.TotalScore
		}
		if entry.Report.MaxPossibleScore > maxPossible {
			maxPossible = entry.Report.MaxPossibleScore
		}
	}

	return &EvaluationAggregate{
		TotalRuns:         len(reports),
		AverageScore:      float64(totalScore) / float64(len(reports)),
		AveragePercentage: totalPercentage / float64(len(reports)),
		HighestScore:      highestScore,
		LowestScore:       lowestScore,
		MaxPossibleScore:  maxPossible,
	}
}

func readWorkflowEvaluationPlan(ctx context.Context, workspacePath string) *string {
	evaluationPlanPath := filepath.Join(workspacePath, "evaluation", "evaluation_plan.json")
	planContent, exists, err := readFileFromWorkspace(ctx, evaluationPlanPath)
	if err != nil || !exists {
		return nil
	}

	var planJSON interface{}
	if err := json.Unmarshal([]byte(planContent), &planJSON); err == nil {
		if formatted, err := json.MarshalIndent(planJSON, "", "  "); err == nil {
			formattedStr := string(formatted)
			return &formattedStr
		}
	}

	return &planContent
}

func (api *StreamingAPI) handleGetWorkflowReviewData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	runFolder := r.URL.Query().Get("run_folder")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	cleanedWorkspacePath := filepath.Clean(workspacePath)
	if strings.Contains(cleanedWorkspacePath, "..") {
		http.Error(w, "Invalid workspace path", http.StatusBadRequest)
		return
	}

	response := workflowReviewDataResponse{
		Success:     true,
		Costs:       loadWorkflowCosts(r.Context(), cleanedWorkspacePath),
		Evaluations: loadWorkflowEvaluationReports(r.Context(), cleanedWorkspacePath, runFolder),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
