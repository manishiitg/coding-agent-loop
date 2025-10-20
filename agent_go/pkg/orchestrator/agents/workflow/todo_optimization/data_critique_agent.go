package todo_optimization

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	workflowmemory "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

	"github.com/tmc/langchaingo/llms"
)

// DataCritiqueTemplate holds template variables for data critique prompts
type DataCritiqueTemplate struct {
	Objective         string
	InputData         string // The data to critique
	InputPrompt       string // The prompt used to generate the data
	RefinementHistory string
	Iteration         int
}

// DataCritiqueAgent extends BaseOrchestratorAgent with data critique functionality
type DataCritiqueAgent struct {
	*agents.BaseOrchestratorAgent // ✅ REUSE: All base functionality
}

// NewDataCritiqueAgent creates a new data critique agent
func NewDataCritiqueAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) *DataCritiqueAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.DataCritiqueAgentType, // 🆕 NEW: Agent type
		eventBridge,
	)

	return &DataCritiqueAgent{
		BaseOrchestratorAgent: baseAgent, // ✅ REUSE: All base functionality
	}
}

// Execute implements the OrchestratorAgent interface
func (dca *DataCritiqueAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract required parameters
	objective, ok := templateVars["objective"]
	if !ok {
		objective = "No objective provided"
	}

	inputData, ok := templateVars["input_data"]
	if !ok {
		inputData = "No input data provided"
	}

	inputPrompt, ok := templateVars["input_prompt"]
	if !ok {
		inputPrompt = "No input prompt provided"
	}

	refinementHistory, ok := templateVars["refinement_history"]
	if !ok {
		refinementHistory = "No refinement history provided"
	}

	iteration := 1
	if iterStr, ok := templateVars["iteration"]; ok {
		var iter int
		if _, err := fmt.Sscanf(iterStr, "%d", &iter); err == nil {
			iteration = iter
		}
	}

	// Prepare template variables
	critiqueTemplateVars := map[string]string{
		"objective":          objective,
		"input_data":         inputData,
		"input_prompt":       inputPrompt,
		"refinement_history": refinementHistory,
		"iteration":          fmt.Sprintf("%d", iteration),
	}

	// Execute using input processor
	return dca.ExecuteWithInputProcessor(ctx, critiqueTemplateVars, dca.dataCritiqueInputProcessor, conversationHistory)
}

// dataCritiqueInputProcessor processes inputs specifically for data critique
func (dca *DataCritiqueAgent) dataCritiqueInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := DataCritiqueTemplate{
		Objective:         templateVars["objective"],
		InputData:         templateVars["input_data"],
		InputPrompt:       templateVars["input_prompt"],
		RefinementHistory: templateVars["refinement_history"],
		Iteration: func() int {
			var iter int
			if _, err := fmt.Sscanf(templateVars["iteration"], "%d", &iter); err == nil {
				return iter
			}
			return 1
		}(),
	}

	// Define the template
	templateStr := `## PRIMARY TASK - CRITIQUE INPUT DATA

You are a data critique agent. Evaluate input data for factual accuracy, analytical quality, and alignment with prompts using MCP servers for validation.

**OBJECTIVE**: {{.Objective}}

**REFINEMENT ITERATION**: {{.Iteration}}

**REFINEMENT HISTORY**:
{{.RefinementHistory}}

**INPUT PROMPT USED TO GENERATE DATA**:
{{.InputPrompt}}

**CURRENT INPUT DATA TO CRITIQUE**:
{{.InputData}}

## Critique Process
1. **Analyze the refinement history** - understand the progression and patterns
2. **Evaluate prompt-output alignment** - how well does the output match the input prompt?
3. **Validate factual accuracy** - use MCP servers to verify claims and data
4. **Assess analytical quality** - depth, reasoning, and logical consistency
5. **Check completeness** - are all prompt requirements fulfilled?
6. **Identify recurring issues** - what problems keep appearing across iterations?
7. **Evaluate refinement effectiveness** - is the refinement process working well?
8. **Provide specific feedback** - actionable suggestions for improvement
9. **Determine if more improvement is needed** - is this refinement satisfactory?

` + workflowmemory.GetWorkflowMemoryRequirements() + `

## Critical Analysis Areas

### 1. Refinement History Analysis
- **How has the data evolved?** (What changes were made in each iteration?)
- **Are improvements building on each other?** (Is each refinement making meaningful progress?)
- **What patterns emerge?** (Do the same issues keep recurring?)
- **Is the refinement process effective?** (Are the right things being refined?)

### 2. Prompt-Output Alignment Assessment
- **Does the output fully address the input prompt?** (All requirements met?)
- **Are the prompt instructions followed correctly?** (Format, structure, content)
- **Is the scope appropriate?** (Not too broad, not too narrow for the prompt)
- **Are success criteria clearly addressed?** (Measurable outcomes defined and met)

### 3. Factual Accuracy Validation
- **Use MCP servers to verify claims** (Web search, database queries, document analysis)
- **Check data accuracy and currency** (Are facts correct and up-to-date?)
- **Validate references and sources** (Are citations and sources reliable?)
- **Identify factual gaps or inaccuracies** (What needs correction?)

### 4. Analytical Quality Assessment (CRITICAL FOCUS)
- **Depth of analysis** - Is the analysis superficial or thorough?
- **Logical reasoning** - Are arguments sound and well-structured?
- **Evidence quality** - Is evidence relevant, sufficient, and properly used?
- **Critical thinking** - Are assumptions questioned and alternatives considered?
- **Insight generation** - Does the analysis provide meaningful insights?
- **Problem-solving approach** - Is the methodology appropriate and effective?
- **Conclusion validity** - Are conclusions well-supported by the analysis?

### 5. Completeness Check
- **Are all prompt requirements fulfilled?** (Nothing essential missing)
- **Are all necessary components included?** (All required sections/elements present)
- **Is the scope appropriate?** (Not too broad, not too narrow for the prompt)
- **Are success criteria clearly addressed?** (Measurable outcomes defined and met)

### 6. Quality Issues Identification
- **Vague descriptions**: Content that is unclear or ambiguous
- **Missing details**: Information that lacks necessary specifics
- **Poor structure**: Content that is disorganized or hard to follow
- **Unrealistic claims**: Statements that are too broad or impossible to verify
- **Missing context**: Information that doesn't explain why it's relevant
- **Factual errors**: Incorrect information that needs correction
- **Weak analysis**: Superficial treatment of complex topics

### 7. Technical Details Assessment (CRITICAL FOR WEB SCRAPING)
- **Command Precision**: Are exact commands, URLs, and selectors specified?
- **Reproducibility**: Can the steps be exactly replicated by another person?
- **Tool Usage**: Are the correct MCP tools specified with proper parameters?
- **Web Scraping Details**: Are URLs, selectors, wait conditions, and data extraction methods clearly defined?
- **Evidence Quality**: Are verification methods and expected outputs specified?
- **Technical Completeness**: Are all technical requirements for execution included?

### 8. Specific Improvement Areas
- **Structure improvements**: Better organization, clearer hierarchy
- **Content improvements**: More specific descriptions, better evidence
- **Analytical improvements**: Deeper analysis, better reasoning
- **Factual improvements**: More accurate information, better sources
- **Focus improvements**: Better alignment with prompt, removal of distractions
- **Technical improvements**: More precise commands, better tool specifications, clearer web scraping details

## Output Format
**IMPORTANT: Return ONLY clean markdown, not JSON or structured data**

**Provide:**
1. **Overall Assessment**: High-level evaluation of the data quality and refinement progression
2. **Refinement History Analysis**: How the data has evolved and what patterns emerge
3. **Prompt-Output Alignment**: How well the output matches the input prompt requirements
4. **Factual Accuracy Assessment**: Results of MCP server validation and fact-checking
5. **Analytical Quality Evaluation**: Depth, reasoning, and critical thinking assessment
6. **Recurring Issues**: Problems that keep appearing across iterations
7. **Issues Found**: Specific problems identified in the current data
8. **Feedback**: Constructive suggestions for improvement
9. **Satisfaction Assessment**: Is this refinement satisfactory or needs more work?

**Output should be:**
- Clean markdown text only
- No JSON structures or code blocks
- Specific, actionable feedback
- Clear assessment of whether more improvement is needed

## Critique Report Format

# Data Critique Report

## Overall Assessment
- **Data Quality**: [Excellent/Good/Fair/Poor] - Brief summary of overall data quality
- **Analytical Quality**: [Excellent/Good/Fair/Poor] - Depth and rigor of analysis
- **Factual Accuracy**: [Excellent/Good/Fair/Poor] - Accuracy of claims and data
- **Prompt Alignment**: [Excellent/Good/Fair/Poor] - How well output matches prompt
- **Progression Quality**: [Excellent/Good/Fair/Poor] - How well the refinement process is working
- **Improvement Level**: [Significant/Moderate/Minimal/None] - How much was actually improved in this iteration

## Refinement History Analysis
### Evolution Patterns
- [How the data has evolved across iterations]
- [What patterns emerge in the refinement process]

### Recurring Issues
- [Problems that keep appearing across iterations]
- [Areas where the refinement process might be ineffective]

## Prompt-Output Alignment Assessment
### Requirements Met
- [List specific prompt requirements that were fulfilled]
- [Areas where the output fully addresses the prompt]

### Requirements Missed
- [List specific prompt requirements that were not met]
- [Areas where the output deviates from the prompt]

## Factual Accuracy Assessment
### MCP Server Validation Results
- [Results from web search, database queries, document analysis]
- [Verified facts and claims]
- [Identified inaccuracies or gaps]

### Source Quality
- [Assessment of references and sources used]
- [Reliability and currency of information]

## Analytical Quality Evaluation
### Depth of Analysis
- [Assessment of analytical depth and thoroughness]
- [Areas where analysis is superficial vs. deep]

### Reasoning Quality
- [Evaluation of logical reasoning and argument structure]
- [Assessment of critical thinking and insight generation]

### Evidence Usage
- [How well evidence is used to support claims]
- [Quality and relevance of supporting data]

## Technical Details Assessment
### Command Precision
- [Are exact commands, URLs, and selectors clearly specified?]
- [Are MCP tool parameters and arguments properly defined?]
- [Are web scraping commands reproducible and specific?]

### Web Scraping Completeness
- [Are URLs, selectors, wait conditions, and data extraction methods clearly defined?]
- [Are screenshots and verification steps specified?]
- [Are expected outputs and success criteria technical enough?]

### Tool Usage Quality
- [Are the correct MCP tools specified for each task?]
- [Are tool parameters and arguments properly documented?]
- [Are alternative approaches mentioned when appropriate?]

### Reproducibility Assessment
- [Can the steps be exactly replicated by another person?]
- [Are all technical dependencies and prerequisites specified?]
- [Are verification methods technical and specific enough?]

## Improvement Analysis
### What Was Improved in This Iteration
- [List specific improvements made in the current refinement]

### What Still Needs Work
- [List areas that still need improvement]

## Issues Found
### Structure Issues
- [List structural problems with the data organization]

### Content Issues
- [List content problems (vague descriptions, missing details, etc.)]

### Analytical Issues
- [List problems with analysis depth, reasoning, or evidence]

### Factual Issues
- [List factual inaccuracies, outdated information, or unreliable sources]

### Alignment Issues
- [List problems with prompt alignment and requirement fulfillment]

### Technical Issues
- [List problems with command precision, tool usage, or technical specifications]
- [List missing technical details like URLs, selectors, or parameters]
- [List reproducibility issues or unclear technical instructions]

## Specific Feedback
### High Priority Improvements
1. [Most important improvement needed]
2. [Second most important improvement needed]
3. [Third most important improvement needed]

### Medium Priority Improvements
- [Additional improvements that would help]

### Low Priority Improvements
- [Nice-to-have improvements]

## Satisfaction Assessment
- **Is this refinement satisfactory?** [Yes/No] - Overall assessment
- **Does it need more improvement?** [Yes/No] - Whether another iteration is needed
- **Is the refinement process effective?** [Yes/No] - Whether the refinement approach is working
- **Is the analytical quality sufficient?** [Yes/No] - Whether the analysis meets standards
- **Key reasons**: [Brief explanation of the assessment]

## Recommendations for Next Iteration
- [Specific actions to take in the next refinement iteration]
- [Focus areas for improvement based on history analysis]
- [Priority order for addressing recurring issues]
- [Suggestions for improving analytical quality and depth]
- [MCP server usage recommendations for fact-checking]
- [Suggestions for improving the refinement process itself]
- [Technical improvements: Add specific URLs, selectors, commands, and tool parameters]
- [Web scraping enhancements: Include wait conditions, screenshots, and data extraction methods]
- [Reproducibility improvements: Make steps more specific and technically detailed]

Focus on providing constructive, specific feedback that will help improve the data quality, analytical rigor, and factual accuracy in the next iteration.`

	// Parse and execute the template
	tmpl, err := template.New("dataCritique").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateData)
	if err != nil {
		return fmt.Sprintf("Error executing template: %v", err)
	}

	return result.String()
}
