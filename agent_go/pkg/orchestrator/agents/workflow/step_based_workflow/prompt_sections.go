package step_based_workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/guidance"
	prompt "github.com/manishiitg/mcpagent/agent/prompt"
)

// BuildStepFilesListing enumerates files in a single step-associated folder (step output
// artifacts, execution logs, etc.) and returns a markdown listing with per-file byte
// sizes. The listing is meant to be inlined into an agent's user message so the agent can
// pick targets without a blind `ls` call.
//
// Layout is flat: hidden files and subdirectories are skipped (every per-step folder in
// this codebase is flat by convention). Returns a terse placeholder when the folder is
// missing or empty — callers typically have fallback language in their prompts for that.
func BuildStepFilesListing(folderPath string) string {
	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return fmt.Sprintf("_Folder not readable at `%s` (%v)._", folderPath, err)
	}
	type fileEntry struct {
		name string
		size int64
	}
	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, infoErr := e.Info()
		if infoErr != nil {
			files = append(files, fileEntry{name: e.Name(), size: -1})
			continue
		}
		files = append(files, fileEntry{name: e.Name(), size: info.Size()})
	}
	if len(files) == 0 {
		return fmt.Sprintf("_Folder `%s` is empty — no files to read._", folderPath)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Files in `%s` (sizes in bytes):\n", folderPath))
	for _, f := range files {
		if f.size < 0 {
			sb.WriteString(fmt.Sprintf("- `%s` (size unknown)\n", f.name))
			continue
		}
		sb.WriteString(fmt.Sprintf("- `%s` (%d bytes)\n", f.name, f.size))
	}
	return sb.String()
}

// PromptSections holds pre-built prompt sections that can be injected into any agent's
// system prompt. All agent types (execution, todo task, evaluation) should
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

// BuildMainPyAuthoringRules returns the canonical rules that any agent writing or
// patching a step's main.py MUST follow. Shared by:
//   - the execution agent in scripted mode (via GetScriptedModeInstructions)
//   - review_step_code (detects drift from these rules)
//   - the parent Pulse Fixer (applies reviewed eval-driven fixes)
//
// The workshop chat agent prompt does NOT call this anymore — it gets a short
// cheat sheet and loads the full rules on demand via
// get_reference_doc(kind="code-authoring") when it actually needs to patch.
//
// Source of truth lives in cmd/server/guidance/templates/system/code-authoring.md;
// this function is a thin wrapper so non-chat agents (which can't call tools to
// load reference docs) still get the rules baked into their prompt.
func BuildMainPyAuthoringRules() string {
	return guidance.RenderSystemDoc("code-authoring") + "\n"
}

// BuildBrowserAuthoringRules returns the browser-automation-specific main.py rules.
// Append to BuildMainPyAuthoringRules() ONLY when the step has agent-browser available.
// Non-browser steps get no benefit from
// these ~60 lines and paying the token tax on every step prompt is wasteful.
//
// Callers: gate with templateVars["HasBrowserAccess"] == "true" or equivalent signal.
func BuildBrowserAuthoringRules() string {
	var sb strings.Builder
	sb.WriteString("## Browser automation rules (this step has agent_browser)\n\n")
	sb.WriteString("**The ONE rule that matters: selectors persisted to main.py must be DETERMINISTIC across future runs.**\n")
	sb.WriteString("A deterministic selector resolves to the same element on every replay — across browser restarts, page rebuilds, deploys that rename auto-generated classes, React key changes, re-hydration. **Refs (`@e1`, `e68`, `\"ref\": \"abc123\"`) are session-local identifiers** the browser tools generate per snapshot; they're reassigned on the next run, so hardcoded refs are the opposite of deterministic. **NEVER write a ref-based selector into main.py, learnings, or any other saved artifact** — the replay will silently click the wrong thing.\n\n")
	sb.WriteString("**Finding deterministic selectors — two paths, both valid:**\n")
	sb.WriteString("- **Path A — `agent_browser` snapshot then act.** Snapshot gives you role + accessible name + state. Parse a live ref from each run's snapshot or choose a deterministic selector (see priority list below), then use `agent_browser` commands such as `click`, `fill`, `select`, and `open`.\n")
	sb.WriteString("- **Path B — DOM probe via `agent_browser` `eval`.** Run the canonical read-only probe (below) to get a structured inventory of the DOM, including pre-filtered deterministic `cssPath` entries (auto-generated ids are filtered out). Use the probe when the accessibility snapshot misses elements (custom `<div>` buttons, portal/popover children, form inputs the tree skips). Eval for discovery is allowed; an action persisted to a saved script must still use a deterministic selector. `document.querySelector('.css-8xy3zb')` is not deterministic.\n\n")
	sb.WriteString("**Deterministic-selector priority for main.py** (pick the highest that uniquely identifies the element and will resolve to the same element on every future run):\n")
	sb.WriteString("  1. `data-testid` / `data-test` / `data-cy` / `data-qa`  (ideal — rare on production sites)\n")
	sb.WriteString("  2. Hand-written, semantic `id` or `name` attribute  (e.g. `#panAdhaarUserId`, `#loginPasswordField`). **Skip auto-generated ids**: `radix-_rN_`, `mat-mdc-*`, `:rNN:`, any UUID-shaped id — these rotate across rebuilds.\n")
	sb.WriteString("  3. `aria-label`  (very durable when present)\n")
	sb.WriteString("  4. Role + accessible name from a fresh snapshot, resolving its live ref at runtime\n")
	sb.WriteString("  5. Label, placeholder, or visible text that is unique and stable\n")
	sb.WriteString("  6. Structural CSS / XPath with nth-child chains  (last resort; flag in learnings)\n")
	sb.WriteString("- **Discovery when the accessibility snapshot is insufficient**: custom `<div>` buttons, dropdowns inside portals, autocomplete options, form inputs missing from the tree. Run a READ-ONLY DOM probe that returns a JSON inventory of the page (role, id, aria-label, data-testid, visible text, stable `cssPath`). One probe tells you the site's hook strategy (e.g. \"38 aria-labels, 0 testids → use aria-label + role+name\"). Then act through `agent_browser` using a live ref or durable selector.\n")
	sb.WriteString("- **Probe invocation**: call `agent_browser` with `command='eval'` and the JavaScript below as its argument. In CDP mode include the configured endpoint and tab arguments required by the browser instructions. Never invoke the CLI through the shell.\n")
	sb.WriteString("- **Canonical DOM probe** — copy this verbatim. Do NOT reinvent it per step; one source of truth keeps results comparable across runs, and the auto-id filtering (radix/mat-mdc/React-useId/UUID) is already tuned:\n")
	sb.WriteString("```javascript\n")
	sb.WriteString("(() => {\n")
	sb.WriteString("  const FLOATING = '[role=\"listbox\"],[role=\"menu\"],[role=\"dialog\"],[role=\"tooltip\"],[data-radix-popper-content-wrapper],[data-floating-ui-portal],[data-headlessui-portal],[data-state=\"open\"]';\n")
	sb.WriteString("  const STABLE = ['data-testid','data-test','data-cy','data-qa','id','name','aria-label','aria-labelledby','placeholder','href','type','role','for'];\n")
	sb.WriteString("  const vis = e => { const r=e.getBoundingClientRect(); if(r.width===0||r.height===0) return false; const s=getComputedStyle(e); return s.visibility!=='hidden'&&s.display!=='none'&&s.opacity!=='0'; };\n")
	sb.WriteString("  const interactive = e => {\n")
	sb.WriteString("    if (['INPUT','TEXTAREA','SELECT','BUTTON','A'].includes(e.tagName)) return true;\n")
	sb.WriteString("    const s=getComputedStyle(e); if(s.cursor==='pointer') return true;\n")
	sb.WriteString("    if(e.onclick||e.getAttribute('onclick')) return true;\n")
	sb.WriteString("    const r=e.getAttribute('role'); return r && ['button','option','menuitem','tab','link','checkbox','radio','switch'].includes(r);\n")
	sb.WriteString("  };\n")
	sb.WriteString("  const autoId = v => v && (/^radix-_r[a-z0-9]+_/.test(v) || /^:r[a-z0-9]+:$/.test(v) || /^mat-mdc-/.test(v) || /[a-f0-9]{8}-[a-f0-9]{4}-/.test(v));\n")
	sb.WriteString("  const describe = e => {\n")
	sb.WriteString("    const a={}; for(const k of STABLE){ const v=e.getAttribute(k); if(v) a[k]=v; }\n")
	sb.WriteString("    const text=(e.innerText||e.value||e.textContent||'').trim().replace(/\\s+/g,' ').slice(0,80);\n")
	sb.WriteString("    let css=null;\n")
	sb.WriteString("    if(a['data-testid']) css=`[data-testid=\"${a['data-testid']}\"]`;\n")
	sb.WriteString("    else if(a['data-test']) css=`[data-test=\"${a['data-test']}\"]`;\n")
	sb.WriteString("    else if(a['data-cy']) css=`[data-cy=\"${a['data-cy']}\"]`;\n")
	sb.WriteString("    else if(a.id && !autoId(a.id) && a.id.length<40) css=`#${CSS.escape(a.id)}`;\n")
	sb.WriteString("    else if(a.name && !autoId(a.name)) css=`[name=\"${a.name}\"]`;\n")
	sb.WriteString("    else if(a['aria-label']) css=`[aria-label=\"${a['aria-label']}\"]`;\n")
	sb.WriteString("    return { tag:e.tagName.toLowerCase(), text, attrs:a, role:e.getAttribute('role')||null, cssPath:css };\n")
	sb.WriteString("  };\n")
	sb.WriteString("  const inv={}; for(const k of STABLE) inv[k]=document.querySelectorAll(`[${k}]`).length;\n")
	sb.WriteString("  const framework = document.querySelector('[data-radix-popper-content-wrapper],[data-state]')?'radix':document.querySelector('mat-icon,mat-select,[class*=\"mat-mdc\"]')?'angular-material':document.querySelector('[data-headlessui-portal]')?'headlessui':(window.React||document.querySelector('[data-reactroot]'))?'react':'unknown';\n")
	sb.WriteString("  const popover=[]; document.querySelectorAll(FLOATING).forEach(c=>{ if(!vis(c)) return; c.querySelectorAll('*').forEach(el=>{ if(!vis(el)||!el.innerText?.trim()) return; popover.push({source:'popover',...describe(el)}); }); });\n")
	sb.WriteString("  const seen=new Set(popover.map(i=>i.cssPath).filter(Boolean));\n")
	sb.WriteString("  const actionable=[]; document.querySelectorAll('body *').forEach(el=>{ if(!vis(el)||!interactive(el)) return; const d=describe(el); if(d.cssPath&&seen.has(d.cssPath)) return; if(!d.text&&!Object.keys(d.attrs).length) return; actionable.push({source:'actionable',...d}); });\n")
	sb.WriteString("  return { url:location.href, framework, stableHookInventory:inv, popoverItems:popover.slice(0,50), actionableItems:actionable.slice(0,120), counts:{popover:popover.length,actionable:actionable.length} };\n")
	sb.WriteString("})()\n")
	sb.WriteString("```\n")
	sb.WriteString("  Returns `{url, framework, stableHookInventory, popoverItems, actionableItems}`. Save `stableHookInventory` + `framework` to learnings as the site profile. Use `actionableItems[i].cssPath` directly in main.py when it's non-null (filtered against auto-generated ids). If `cssPath` is null, fall back to role+name from the a11y snapshot.\n")
	sb.WriteString("- **Site-access resilience**: if a headless `open` returns \"Permission Denied\", a blank page, or a native-alert freeze, switch the workflow to CDP mode against an existing Chrome and document the precondition in learnings. Register a dialog handler before interacting if the page shows native alerts.\n")
	sb.WriteString("- Wait by polling snapshots in a loop checking for expected content / expected widget state (e.g. disabled→enabled). NOT `time.sleep(N)` for UI state (use short sleeps 1-2s only between polls).\n")
	sb.WriteString("- On failure (element missing, navigation stuck), print **both** the current snapshot AND the last probe result (if any) so the fix loop sees both views.\n")
	sb.WriteString("- Call `get_api_spec` to discover exact parameter schemas — don't guess parameter names.\n")
	sb.WriteString("\n")
	return sb.String()
}

// browserCapabilityProvider is the minimal interface BuildBrowserAuthoringRules helpers
// need from the orchestrator/controller to decide whether to emit browser rules.
// We check HasBrowserCapability (not GetBrowserMode) because empty browserMode means
// "auto-detect", not "no browser".
type browserCapabilityProvider interface {
	HasBrowserCapability() bool
}

// browserAuthoringRulesIfBrowserEnabled returns BuildBrowserAuthoringRules() when the
// workflow has the agent-browser skill or a CDP port, else "". Use at call sites that have access to
// the orchestrator (e.g. workshop manager, planning exports).
func browserAuthoringRulesIfBrowserEnabled(p browserCapabilityProvider) string {
	if p == nil || !p.HasBrowserCapability() {
		return ""
	}
	return BuildBrowserAuthoringRules()
}

// BrowserAuthoringRulesFromTemplateVars returns BuildBrowserAuthoringRules() when
// templateVars["HasBrowserAccess"] is "true", else "". Use at call sites that don't
// have direct access to the orchestrator (e.g. agent Execute methods that receive
// only templateVars, including review_step_code).
func BrowserAuthoringRulesFromTemplateVars(templateVars map[string]string) string {
	if templateVars["HasBrowserAccess"] == "true" {
		return BuildBrowserAuthoringRules()
	}
	return ""
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
	sb.WriteString("execution_dir = os.environ['STEP_EXECUTION_DIR']  # parent folder for sibling-step reads only (fallback only — prefer sys.argv for input data)\n")
	sb.WriteString("db_path       = os.environ['DB_PATH']             # ABSOLUTE path to the workflow db/db.sqlite — ALWAYS use this for sqlite, never relative 'db/db.sqlite' (the step's CWD is not the workflow root)\n")
	sb.WriteString("mcp_url       = os.environ['MCP_API_URL']\n")
	sb.WriteString("mcp_token     = os.environ['MCP_API_TOKEN']\n")
	sb.WriteString("group_name    = os.environ.get('VAR_GROUP_NAME', '')  # current group name (e.g., 'production'); empty if no group\n")
	sb.WriteString("```\n\n")
	sb.WriteString("**sqlite `unable to open database file`?** First verify that `DB_PATH` exists, is absolute, and points to the workflow db, then pass it directly to `sqlite3.connect(os.environ['DB_PATH'])`. Never use relative `db/db.sqlite`, generate `.sql` files, copy the db to `/tmp`, or silently switch to the `sqlite3` CLI as a workaround. If the absolute path exists but Python still cannot open it, report a Runloop runtime/folder-guard failure with the exact path and error; do not describe Python sqlite as generally sandbox-blocked.\n\n")

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
			prevStepExecPath := getExecutionFolderPath(executionWorkspacePath, allSteps[j].GetID(), prevStepPath)
			appendCandidate(buildDepAbsPath(prevStepExecPath, dep))
		}
	}

	if cut := strings.LastIndex(currentStepPath, "-sub-"); cut != -1 {
		if stepIndex >= 0 && stepIndex < len(allSteps) {
			if todoStep, ok := allSteps[stepIndex].(*TodoTaskPlanStep); ok {
				parentRoutePrefix := fmt.Sprintf("step-%d-sub-", stepIndex+1)
				currentTodoIDPart := ""
				parentStepPath := fmt.Sprintf("step-%d", stepIndex+1)
				for _, route := range todoStep.PredefinedRoutes {
					routePrefix := fmt.Sprintf("%s%s-", parentRoutePrefix, workflowSafeIDPart(route.RouteID, "route"))
					if strings.HasPrefix(currentStepPath, routePrefix) {
						currentTodoIDPart = strings.TrimPrefix(currentStepPath, routePrefix)
						break
					}
				}
				for _, route := range todoStep.PredefinedRoutes {
					if route.SubAgentStep == nil {
						continue
					}
					routeOutput := ResolveVariables(route.SubAgentStep.GetContextOutput().String(), variableValues)
					if !contextOutputMatchesDependency(routeOutput, dep) {
						continue
					}
					if currentTodoIDPart != "" {
						routeStepPath := todoSubAgentArtifactFolderName(parentStepPath, route.RouteID, currentTodoIDPart)
						routeExecPath := getExecutionFolderPath(executionWorkspacePath, "", routeStepPath)
						appendCandidate(buildDepAbsPath(routeExecPath, dep))
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
//   - Desktop DMG (Mac): set by desktop/main.js → "~/Library/Application Support/Runloop/workspace-docs"
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
		if p == "" || docsRoot == "" || filepath.IsAbs(p) {
			result[i] = p
		} else {
			result[i] = filepath.Join(docsRoot, p)
		}
	}
	return result
}
