package step_based_workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	prompt "github.com/manishiitg/mcpagent/agent/prompt"
)

// PromptSections holds pre-built prompt sections that can be injected into any agent's
// system prompt. All agent types (execution, todo task, conditional, evaluation) should
// use these common builders for consistency.
type PromptSections struct {
	CodeExecution string // Code execution or tool search mode instructions
	Learnings     string // Formatted learning history section
	PreviousSteps string // Previous steps context section
}

// BuildCodeExecutionSection returns the code execution mode instructions.
// isCodeExecution: agent uses code execution mode (HTTP API calls via shell)
// workspacePath: absolute workspace path for code examples
func BuildCodeExecutionSection(isCodeExecution bool, workspacePath string) string {
	if isCodeExecution {
		return prompt.GetCodeExecutionInstructions(workspacePath)
	}
	return ""
}

// BuildLearningsSection returns the formatted learning history section for the system prompt.
// learningHistory: the formatted learning content (empty string means no learnings)
// keepLearningFull: whether full learning content is included (vs paths-only)
func BuildLearningsSection(learningHistory string, keepLearningFull bool) string {
	if learningHistory == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Learning History (Secondary Guidance)\n")
	sb.WriteString(learningHistory)
	sb.WriteString("\n\n")
	sb.WriteString("- **Workflows**: Use validated sequences from learnings, but adapt args to this specific step.\n")
	sb.WriteString("- **Patterns**: Use tool hints/error recovery patterns from learnings.\n")
	sb.WriteString("- **Conflict**: If learning conflicts with step requirement, the step wins.\n")
	if !keepLearningFull {
		sb.WriteString("- **Note**: These learnings are incomplete. Rely primarily on the step description and your own capabilities.\n")
	}

	return sb.String()
}

// BuildPreviousStepsSection returns the previous steps context section for the system prompt.
// previousStepsSummary: the formatted summary from buildPreviousStepsSummary()
func BuildPreviousStepsSection(previousStepsSummary string) string {
	if previousStepsSummary == "" {
		return ""
	}
	return previousStepsSummary
}

// BuildVariablesSection returns the variables section for the system prompt.
// variableNames: formatted variable names (empty if no variables)
// variableValues: formatted variable values (empty if no values)
func BuildVariablesSection(variableNames string, variableValues string) string {
	if variableNames == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Variables\n")
	sb.WriteString(variableNames)
	sb.WriteString("\n")
	if variableValues != "" {
		sb.WriteString(fmt.Sprintf("**Values**: %s\n", variableValues))
	}
	sb.WriteString("\n**Handling**: Step descriptions are already resolved. For code and tool calls, use the resolved values directly.\n")
	return sb.String()
}

func contextOutputMatchesDependency(output string, dep string) bool {
	if strings.TrimSpace(output) == strings.TrimSpace(dep) {
		return true
	}
	for _, part := range strings.Split(output, ",") {
		if strings.TrimSpace(part) == strings.TrimSpace(dep) {
			return true
		}
	}
	return false
}

// BuildPythonBestPractices returns a "Python Best Practices" section for code execution agents.
// varMappingLines lists {{VAR}} → SECRET_VAR mappings (may be empty).
// hasInputArgs: whether the step has positional input file args (sys.argv).
// This is the single source of truth for Python code patterns so all generated scripts are consistent.
func BuildPythonBestPractices(varMappingLines []string, hasInputArgs bool) string {
	var sb strings.Builder
	sb.WriteString("\n## Python Best Practices\n\n")
	sb.WriteString("Use these exact patterns for consistency across all scripts.\n\n")

	// Env vars / secrets
	sb.WriteString("### Accessing secrets and workflow variables\n")
	sb.WriteString("```python\n")
	sb.WriteString("import os, sys\n\n")
	sb.WriteString("# Always use os.environ['KEY'] — never os.environ.get('KEY', 'default')\n")
	sb.WriteString("# Missing var = KeyError (fail loudly, never silently fall back to a hardcoded value)\n\n")
	sb.WriteString("# Workflow variables → VAR_<NAME>  (non-secret config: user IDs, sheet IDs, etc.)\n")
	if len(varMappingLines) > 0 {
		for _, line := range varMappingLines {
			// line format: "{{VAR}} → os.environ['VAR_VAR']"
			parts := strings.SplitN(line, " → ", 2)
			if len(parts) == 2 {
				varName := strings.Trim(parts[0], "{}")
				sb.WriteString(fmt.Sprintf("%s = os.environ['VAR_%s']\n", strings.ToLower(varName), varName))
			}
		}
	} else {
		sb.WriteString("my_var = os.environ['VAR_MY_VAR']\n")
	}
	sb.WriteString("\n# Real secrets → SECRET_<NAME>  (passwords, API keys, tokens)\n")
	sb.WriteString("my_password = os.environ['SECRET_MY_PASSWORD']\n")
	sb.WriteString("\n# Special vars always available:\n")
	sb.WriteString("output_dir    = os.environ['STEP_OUTPUT_DIR']      # write all output files here\n")
	sb.WriteString("execution_dir = os.environ['STEP_EXECUTION_DIR']  # parent folder (fallback only — prefer sys.argv for input data)\n")
	sb.WriteString("mcp_url       = os.environ['MCP_API_URL']\n")
	sb.WriteString("mcp_token     = os.environ['MCP_API_TOKEN']\n")
	sb.WriteString("```\n\n")

	// Input files
	if hasInputArgs {
		sb.WriteString("### Reading input files (positional args)\n")
		sb.WriteString("```python\n")
		sb.WriteString("import sys, json\n\n")
		sb.WriteString("input_file = sys.argv[1]          # first context_dependency path\n")
		sb.WriteString("# input_file2 = sys.argv[2]       # second, if any\n")
		sb.WriteString("with open(input_file) as f:\n")
		sb.WriteString("    data = json.load(f)            # or f.read() for plain text\n")
		sb.WriteString("```\n\n")
	}

	// MCP tool call
	sb.WriteString("### Calling an MCP tool\n")
	sb.WriteString("```python\n")
	sb.WriteString("import requests, os, json, time\n\n")
	sb.WriteString("VERBOSE = os.environ.get('SCRIPT_VERBOSE', '') == '1'\n\n")
	sb.WriteString("def call_mcp(server, tool, args, retries=3, backoff=2):\n")
	sb.WriteString("    \"\"\"Call an MCP tool via HTTP. Retries on broken pipe / connection errors.\"\"\"\n")
	sb.WriteString("    url = os.environ['MCP_API_URL'] + f'/tools/mcp/{server}/{tool}'\n")
	sb.WriteString("    headers = {\n")
	sb.WriteString("        'Authorization': f'Bearer {os.environ[\"MCP_API_TOKEN\"]}',\n")
	sb.WriteString("        'Content-Type': 'application/json',\n")
	sb.WriteString("    }\n")
	sb.WriteString("    if VERBOSE: print(f'[MCP] >> {server}/{tool} args={json.dumps(args)[:500]}')\n")
	sb.WriteString("    last_err = None\n")
	sb.WriteString("    for attempt in range(retries):\n")
	sb.WriteString("        try:\n")
	sb.WriteString("            resp = requests.post(url, json=args, headers=headers, timeout=120)\n")
	sb.WriteString("            resp.raise_for_status()\n")
	sb.WriteString("            result = resp.json()\n")
	sb.WriteString("            if not result.get('success'):\n")
	sb.WriteString("                err = result.get('error', '')\n")
	sb.WriteString("                if VERBOSE: print(f'[MCP] !! {server}/{tool} FAILED: {err[:1000]}')\n")
	sb.WriteString("                # Broken pipe from Go's MCP connection — retry, the server will reconnect\n")
	sb.WriteString("                if 'broken pipe' in err.lower() or 'connection reset' in err.lower() or 'transport closed' in err.lower():\n")
	sb.WriteString("                    last_err = RuntimeError(f'MCP broken pipe: {err}')\n")
	sb.WriteString("                    if attempt < retries - 1:\n")
	sb.WriteString("                        time.sleep(backoff * (attempt + 1))\n")
	sb.WriteString("                        continue\n")
	sb.WriteString("                raise RuntimeError(f'MCP error: {err}')\n")
	sb.WriteString("            res = result['result']\n")
	sb.WriteString("            if VERBOSE: print(f'[MCP] << {server}/{tool} OK ({len(str(res))} chars): {str(res)[:500]}')\n")
	sb.WriteString("            return res\n")
	sb.WriteString("        except (requests.exceptions.ConnectionError, requests.exceptions.Timeout) as e:\n")
	sb.WriteString("            if VERBOSE: print(f'[MCP] !! {server}/{tool} attempt {attempt+1} error: {e}')\n")
	sb.WriteString("            last_err = e\n")
	sb.WriteString("            if attempt < retries - 1:\n")
	sb.WriteString("                time.sleep(backoff * (attempt + 1))\n")
	sb.WriteString("    raise last_err\n")
	sb.WriteString("```\n\n")

	// Writing output files
	sb.WriteString("### Writing output files\n")
	sb.WriteString("```python\n")
	sb.WriteString("import os, json\n\n")
	sb.WriteString("output_dir = os.environ['STEP_OUTPUT_DIR']\n")
	sb.WriteString("os.makedirs(output_dir, exist_ok=True)\n\n")
	sb.WriteString("# JSON output:\n")
	sb.WriteString("with open(os.path.join(output_dir, 'result.json'), 'w') as f:\n")
	sb.WriteString("    json.dump(data, f, indent=2)\n\n")
	sb.WriteString("# Text output:\n")
	sb.WriteString("with open(os.path.join(output_dir, 'output.txt'), 'w') as f:\n")
	sb.WriteString("    f.write(text)\n")
	sb.WriteString("```\n\n")

	// Error diagnostics guidance
	sb.WriteString("### Error diagnostics (critical for fix loop)\n")
	sb.WriteString("When your script fails, the **only** feedback the system sees is stdout + stderr.\n")
	sb.WriteString("Files written to disk are **not** automatically read back. So:\n")
	sb.WriteString("- **Always `print()` diagnostic context before raising/exiting on failure** — e.g., current page snapshot, API response body, intermediate state, what you expected vs. what you got.\n")
	sb.WriteString("- Never write debug info only to a file (the system won't read it). Print it to stdout first, then optionally save to a file.\n")
	sb.WriteString("- Include enough context that a future fix attempt can pinpoint the root cause without re-running the script.\n")
	sb.WriteString("```python\n")
	sb.WriteString("# BAD — debug info only in a file, invisible to the fix loop:\n")
	sb.WriteString("with open('debug.txt', 'w') as f: f.write(snapshot)\n")
	sb.WriteString("raise RuntimeError('Failed — check debug.txt')  # fix loop can't read debug.txt!\n\n")
	sb.WriteString("# GOOD — print diagnostic context so the fix loop can see it:\n")
	sb.WriteString("print(f'[DIAG] Expected: dashboard page, Got: {current_state}')\n")
	sb.WriteString("print(f'[DIAG] Page content (first 2000 chars):\\n{snapshot[:2000]}')\n")
	sb.WriteString("raise RuntimeError('Login failed — not on dashboard')\n")
	sb.WriteString("```\n")

	return sb.String()
}

// ResolveDependencyPathCandidates returns candidate absolute paths for a dependency, ordered by
// workflow likelihood. Callers can optionally verify these against the real workspace and pick
// the first existing file.
func ResolveDependencyPathCandidates(
	dep string,
	stepIndex int,
	currentStepPath string,
	allSteps []PlanStepInterface,
	executionWorkspacePath string,
	docsRoot string,
	variableValues map[string]string,
) []string {
	if dep == "" {
		return nil
	}
	toAbs := func(path string) string {
		if path == "" || docsRoot == "" {
			return path
		}
		return filepath.Join(docsRoot, path)
	}
	buildDepAbsPath := func(folderPath string, dep string) string {
		return fmt.Sprintf("%s/%s", toAbs(folderPath), dep)
	}
	currentStepID := ""
	if stepIndex >= 0 && stepIndex < len(allSteps) {
		currentStepID = allSteps[stepIndex].GetID()
	}
	if currentStepPath == "" {
		currentStepPath = fmt.Sprintf("step-%d", stepIndex+1)
	}

	if filepath.IsAbs(dep) || strings.Contains(dep, "/") {
		return []string{dep}
	}

	candidates := make([]string, 0, 3)
	appendCandidate := func(candidate string) {
		if candidate == "" {
			return
		}
		for _, existing := range candidates {
			if existing == candidate {
				return
			}
		}
		candidates = append(candidates, candidate)
	}

	for j := 0; j < stepIndex && j < len(allSteps); j++ {
		prevOutput := ResolveVariables(allSteps[j].GetContextOutput().String(), variableValues)
		if contextOutputMatchesDependency(prevOutput, dep) {
			prevStepPath := fmt.Sprintf("step-%d", j+1)
			if allSteps[j].StepType() == StepTypeDecision {
				prevStepPath = fmt.Sprintf("step-%d-decision", j+1)
			}
			prevStepExecPath := getExecutionFolderPath(executionWorkspacePath, allSteps[j].GetID(), prevStepPath)
			appendCandidate(buildDepAbsPath(prevStepExecPath, dep))
		}
	}

	if cut := strings.LastIndex(currentStepPath, "-sub-"); cut != -1 {
		if stepIndex >= 0 && stepIndex < len(allSteps) {
			if todoStep, ok := allSteps[stepIndex].(*TodoTaskPlanStep); ok {
				parentRoutePrefix := fmt.Sprintf("step-%d-sub-", stepIndex+1)
				for _, route := range todoStep.PredefinedRoutes {
					if route.SubAgentStep == nil {
						continue
					}
					routeOutput := ResolveVariables(route.SubAgentStep.GetContextOutput().String(), variableValues)
					if !contextOutputMatchesDependency(routeOutput, dep) {
						continue
					}
					routeStepPath := parentRoutePrefix + route.RouteID
					routeExecPath := getExecutionFolderPath(executionWorkspacePath, route.SubAgentStep.GetID(), routeStepPath)
					appendCandidate(buildDepAbsPath(routeExecPath, dep))
				}
			}
		}

		parentStepPath := currentStepPath[:cut]
		parentStepID := ""
		if parentStepPath == fmt.Sprintf("step-%d", stepIndex+1) {
			parentStepID = currentStepID
		}
		parentStepExecPath := getExecutionFolderPath(executionWorkspacePath, parentStepID, parentStepPath)
		appendCandidate(buildDepAbsPath(parentStepExecPath, dep))
	}

	currentStepExecPath := getExecutionFolderPath(executionWorkspacePath, currentStepID, currentStepPath)
	appendCandidate(buildDepAbsPath(currentStepExecPath, dep))

	if len(candidates) == 0 {
		return []string{dep}
	}
	return candidates
}

// ResolveDependencyPaths maps dependency filenames to the most likely absolute path based on the
// workflow plan. This is the common logic used by both execution and todo task agents to show
// full paths instead of bare filenames.
func ResolveDependencyPaths(
	deps []string,
	stepIndex int,
	currentStepPath string,
	allSteps []PlanStepInterface,
	executionWorkspacePath string,
	docsRoot string,
	variableValues map[string]string,
) []string {
	if len(deps) == 0 {
		return nil
	}

	fullPathDeps := make([]string, 0, len(deps))
	for _, dep := range deps {
		candidates := ResolveDependencyPathCandidates(dep, stepIndex, currentStepPath, allSteps, executionWorkspacePath, docsRoot, variableValues)
		if len(candidates) == 0 {
			fullPathDeps = append(fullPathDeps, dep)
			continue
		}
		fullPathDeps = append(fullPathDeps, candidates[0])
	}
	return fullPathDeps
}

// GetPromptDocsRoot returns the workspace docs root path for use in prompts.
// This path is passed to LLM agents so they generate correct absolute paths in
// shell commands (jq, cat, ls, etc.) that execute inside the workspace server.
//
// Deployment modes:
//   - Docker (default):  not set → returns "/app/workspace-docs" (volume mount inside container)
//   - Desktop DMG (Mac): set by desktop/main.js → "~/Library/Application Support/AgentForge/workspace-docs"
//     (workspace-server runs as a native binary, no Docker)
//   - run_server_with_logging.sh: NOT set, because workspace still runs in Docker
//
// ~30 callers across the workflow engine use this; change the env var, not callers.
func GetPromptDocsRoot() string {
	if p := os.Getenv("WORKSPACE_DOCS_PATH"); p != "" {
		return filepath.Clean(p)
	}
	return "/app/workspace-docs"
}

// toAbsPaths converts a slice of workspace-relative paths to absolute paths by prepending docsRoot.
func toAbsPaths(docsRoot string, paths []string) []string {
	result := make([]string, len(paths))
	for i, p := range paths {
		if p == "" || docsRoot == "" {
			result[i] = p
		} else {
			result[i] = filepath.Join(docsRoot, p)
		}
	}
	return result
}
