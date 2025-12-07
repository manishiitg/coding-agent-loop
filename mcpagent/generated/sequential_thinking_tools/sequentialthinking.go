package sequential_thinking_tools

import (
	"encoding/json"
	"fmt"
)

type SequentialthinkingParams struct {
	// Branching point thought number
	BranchFromThought *int `json:"branchFromThought,omitempty"`
	// Branch identifier
	BranchId *string `json:"branchId,omitempty"`
	// Whether another thought step is needed
	NextThoughtNeeded *bool `json:"nextThoughtNeeded,omitempty"`
	// Which thought is being reconsidered
	RevisesThought *int `json:"revisesThought,omitempty"`
	// Your current thinking step
	Thought *string `json:"thought,omitempty"`
	// Current thought number (numeric value, e.g., 1, 2, 3)
	ThoughtNumber *int `json:"thoughtNumber,omitempty"`
	// Whether this revises previous thinking
	IsRevision *bool `json:"isRevision,omitempty"`
	// If more thoughts are needed
	NeedsMoreThoughts *bool `json:"needsMoreThoughts,omitempty"`
	// Estimated total thoughts needed (numeric value, e.g., 5, 10)
	TotalThoughts *int `json:"totalThoughts,omitempty"`
}

// A detailed tool for dynamic and reflective problem-solving through thoughts.
// This tool helps analyze problems through a flexible thinking process that can adapt and evolve.
// Each thought can build on, question, or revise previous insights as understanding deepens.
// 
// When to use this tool:
// - Breaking down complex problems into steps
// - Planning and design with room for revision
// - Analysis that might need course correction
// - Problems where the full scope might not be clear initially
// - Problems that require a multi-step solution
// - Tasks that need to maintain context over multiple steps
// - Situations where irrelevant information needs to be filtered out
// 
// Key features:
// - You can adjust total_thoughts up or down as you progress
// - You can question or revise previous thoughts
// - You can add more thoughts even after reaching what seemed like the end
// - You can express uncertainty and explore alternative approaches
// - Not every thought needs to build linearly - you can branch or backtrack
// - Generates a solution hypothesis
// - Verifies the hypothesis based on the Chain of Thought steps
// - Repeats the process until satisfied
// - Provides a correct answer
// 
// Parameters explained:
// - thought: Your current thinking step, which can include:
//   * Regular analytical steps
//   * Revisions of previous thoughts
//   * Questions about previous decisions
//   * Realizations about needing more analysis
//   * Changes in approach
//   * Hypothesis generation
//   * Hypothesis verification
// - nextThoughtNeeded: True if you need more thinking, even if at what seemed like the end
// - thoughtNumber: Current number in sequence (can go beyond initial total if needed)
// - totalThoughts: Current estimate of thoughts needed (can be adjusted up/down)
// - isRevision: A boolean indicating if this thought revises previous thinking
// - revisesThought: If is_revision is true, which thought number is being reconsidered
// - branchFromThought: If branching, which thought number is the branching point
// - branchId: Identifier for the current branch (if any)
// - needsMoreThoughts: If reaching end but realizing more thoughts needed
// 
// You should:
// 1. Start with an initial estimate of needed thoughts, but be ready to adjust
// 2. Feel free to question or revise previous thoughts
// 3. Don't hesitate to add more thoughts if needed, even at the "end"
// 4. Express uncertainty when present
// 5. Mark thoughts that revise previous thinking or branch into new paths
// 6. Ignore information that is irrelevant to the current step
// 7. Generate a solution hypothesis when appropriate
// 8. Verify the hypothesis based on the Chain of Thought steps
// 9. Repeat the process until satisfied with the solution
// 10. Provide a single, ideally correct answer as the final output
// 11. Only set next_thought_needed to false when truly done and a satisfactory answer is reached
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'sequential-thinking'
//          output, err := Sequentialthinking(SequentialthinkingParams{
//              BranchFromThought: "value",
//              // ... other parameters
//          })
//
func Sequentialthinking(params SequentialthinkingParams) (string, error) {
	// Convert params struct to map for API call
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("failed to marshal parameters: %w", err)
	}
	var paramsMap map[string]interface{}
	if err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {
		return "", fmt.Errorf("failed to unmarshal parameters: %w", err)
	}

	// Build request payload and call common API client
	payload := map[string]interface{}{
		"server": "sequential-thinking",
		"tool":   "sequentialthinking",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

