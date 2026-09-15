package step_based_workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func reportToolReadFile(files map[string]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, path string) (string, error) {
		content, ok := files[path]
		if !ok {
			return "", fmt.Errorf("file not found: %s", path)
		}
		return content, nil
	}
}

func validateReport(t *testing.T, html string, hooks ReportHTMLValidationHooks) string {
	t.Helper()
	agent := newWorkshopDefinitionDraft()
	const workspace = "Workflow/demo"
	files := map[string]string{"Workflow/demo/db/reports/index.html": html}
	if err := registerHTMLReportTools(agent, workspace, workshopToolTestLogger{}, reportToolReadFile(files), hooks); err != nil {
		t.Fatalf("registerHTMLReportTools: %v", err)
	}
	tool := agent.tools["validate_report_html"]
	if tool.Execute == nil {
		t.Fatal("validate_report_html was not registered")
	}
	result, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("validate page: %v", err)
	}
	return result
}

func TestValidateHTMLReportRequiresTheSingleWorkflowDocument(t *testing.T) {
	t.Parallel()
	result := validateReport(t, "<!doctype html><html><head><title>Daily briefing</title></head><body>OK</body></html>", ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": true`) || !strings.Contains(result, "Daily briefing") {
		t.Fatalf("unexpected validation result: %s", result)
	}
	if !strings.Contains(result, `"sql_check_enabled": false`) || !strings.Contains(result, `"path_check_enabled": false`) {
		t.Fatalf("expected the result to say the runtime checks were skipped without hooks: %s", result)
	}
}

func TestValidateHTMLReportAcceptsAnotherReportDocument(t *testing.T) {
	t.Parallel()
	agent := newWorkshopDefinitionDraft()
	const workspace = "Workflow/demo"
	files := map[string]string{
		"Workflow/demo/db/reports/tasks.html": "<!doctype html><html><head><title>Tasks</title></head><body>OK</body></html>",
	}
	if err := registerHTMLReportTools(agent, workspace, workshopToolTestLogger{}, reportToolReadFile(files), ReportHTMLValidationHooks{}); err != nil {
		t.Fatal(err)
	}
	result, err := agent.tools["validate_report_html"].Execute(context.Background(), map[string]interface{}{"document_path": "db/reports/tasks.html"})
	if err != nil || !strings.Contains(result, `"path": "db/reports/tasks.html"`) {
		t.Fatalf("validate alternate document: %v %s", err, result)
	}
	if _, err := agent.tools["validate_report_html"].Execute(context.Background(), map[string]interface{}{"document_path": "db/reports/../secret.html"}); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestValidateHTMLReportRejectsImmediateWritesToMissingElements(t *testing.T) {
	t.Parallel()
	result := validateReport(t, `<!doctype html><html><head><title>Combined report</title></head><body><div id="lat-asof"></div><script>document.getElementById('asof').textContent = 'ready'</script></body></html>`, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": false`) || !strings.Contains(result, `missing element id \"asof\"`) {
		t.Fatalf("unexpected validation result: %s", result)
	}
}

func TestValidateHTMLReportRunsLiteralQueriesAgainstTheDatabase(t *testing.T) {
	t.Parallel()
	html := "<!doctype html><html><head><title>Runs</title></head><body><script>" +
		"window.report.ready(async function(){" +
		"  var a = await window.report.query('SELECT id FROM runs ORDER BY id DESC LIMIT 5');" +
		"  var b = await window.report.query(\"SELECT count(*) AS n FROM run_summaries\");" +
		"  var c = await window.report.query(`SELECT * FROM leads WHERE status = 'new'`);" +
		"  var d = await window.report.query(`SELECT * FROM ${table}`);" +
		"  var e = await window.report.query(builtSql);" +
		"});</script></body></html>"
	seen := []string{}
	hooks := ReportHTMLValidationHooks{
		ExplainSQL: func(_ context.Context, sql string) error {
			seen = append(seen, sql)
			if strings.Contains(sql, "run_summaries") {
				return errors.New("no such table: run_summaries")
			}
			return nil
		},
	}
	result := validateReport(t, html, hooks)
	if len(seen) != 3 {
		t.Fatalf("expected the three literal queries to be explained, got %d: %v", len(seen), seen)
	}
	if !strings.Contains(result, `"valid": false`) || !strings.Contains(result, "no such table: run_summaries") {
		t.Fatalf("expected the failing query to be reported: %s", result)
	}
	if !strings.Contains(result, `"sql_literals": 3`) || !strings.Contains(result, `"sql_unchecked": 2`) {
		t.Fatalf("expected the result to count checked and unchecked queries: %s", result)
	}
}

// The sales-outreach report (and most agent-authored ones) never call
// window.report.query with a literal: they wrap it once and pass SQL to the
// wrapper. Those statements must still be checked.
func TestValidateHTMLReportChecksSQLPassedThroughALocalWrapper(t *testing.T) {
	t.Parallel()
	html := "<!doctype html><html><head><title>Leads</title></head><body><script>" +
		"async function query(sql){ return normalize(await window.report.query(sql)); }" +
		"async function load(){" +
		"  const leads = await query(`SELECT id, company FROM leads WHERE status = 'new' ORDER BY id DESC`);" +
		"  const gone = await query('select count(*) as n from missing_table');" +
		"  const label = 'Select a market';" + // prose, not SQL
		"  const strategy = \"<!DOCTYPE html><p>select nothing here</p>\";" + // markup, not SQL
		"}</script></body></html>"
	seen := []string{}
	hooks := ReportHTMLValidationHooks{
		ExplainSQL: func(_ context.Context, sql string) error {
			seen = append(seen, sql)
			if strings.Contains(sql, "missing_table") {
				return errors.New("no such table: missing_table")
			}
			return nil
		},
	}
	result := validateReport(t, html, hooks)
	if len(seen) != 2 {
		t.Fatalf("expected the two wrapper-passed statements to be explained, got %d: %v", len(seen), seen)
	}
	if !strings.Contains(result, `"valid": false`) || !strings.Contains(result, "no such table: missing_table") {
		t.Fatalf("expected the failing wrapped query to be reported: %s", result)
	}
	if !strings.Contains(result, `"sql_literals": 2`) {
		t.Fatalf("expected two checked statements: %s", result)
	}
}

func TestValidateHTMLReportChecksReferencedFilesAndExternalAssets(t *testing.T) {
	t.Parallel()
	html := `<!doctype html><html><head><title>Assets</title>
<link rel="stylesheet" href="https://cdn.example.com/x.css">
</head><body>
<img src="db/assets/logo.png"><a href="db/reports/proof.pdf">proof</a>
<script>window.report.ready(async()=>{ el.innerHTML = await window.report.getHtml('db/notes/summary.md'); window.report.openFile("db/assets/logo.png") })</script>
</body></html>`
	hooks := ReportHTMLValidationHooks{
		FileExists: func(_ context.Context, path string) (bool, error) {
			return path == "db/assets/logo.png", nil
		},
	}
	result := validateReport(t, html, hooks)
	for _, want := range []string{
		`"valid": false`,
		`referenced file \"db/notes/summary.md\" does not exist`,
		`referenced file \"db/reports/proof.pdf\" does not exist`,
		"external stylesheet/script URL found",
		`"referenced_paths": 3`,
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected %q in result: %s", want, result)
		}
	}
	if strings.Contains(result, `\"db/assets/logo.png\" does not exist`) {
		t.Fatalf("existing asset must not be reported: %s", result)
	}
}

func TestValidateHTMLReportRejectsTopLevelWritesBeforeTheirElement(t *testing.T) {
	t.Parallel()
	html := `<!doctype html><html><head><title>Order</title>
<script>document.getElementById('cards').textContent = 'x';</script>
</head><body><div id="cards"></div></body></html>`
	result := validateReport(t, html, ReportHTMLValidationHooks{})
	for _, want := range []string{
		`"valid": false`,
		`before that element appears in the document`,
		`(line 2)`,
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected %q in result: %s", want, result)
		}
	}
}

func TestValidateHTMLReportAcceptsDeferredAndOrderedWrites(t *testing.T) {
	t.Parallel()
	readyWrapped := `<!doctype html><html><head><title>Ready</title>
<script>window.report.ready(function(){ document.getElementById('cards').textContent = 'x'; });</script>
</head><body><div id="cards"></div></body></html>`
	namedFn := `<!doctype html><html><head><title>Fn</title></head><body>
<button onclick="fill()">x</button>
<script>function fill(){ document.getElementById('cards').textContent = 'x'; }</script>
<div id="cards"></div></body></html>`
	ordered := `<!doctype html><html><head><title>Ordered</title></head><body>
<div id="cards"></div>
<script>document.getElementById('cards').textContent = 'x';</script>
</body></html>`
	for name, html := range map[string]string{"ready": readyWrapped, "namedFn": namedFn, "ordered": ordered} {
		result := validateReport(t, html, ReportHTMLValidationHooks{})
		if !strings.Contains(result, `"valid": true`) {
			t.Fatalf("%s: expected valid report: %s", name, result)
		}
		if strings.Contains(result, "before that element appears") {
			t.Fatalf("%s: unexpected ordering error: %s", name, result)
		}
	}
}

func TestValidateHTMLReportRejectsCallsToUndefinedFunctions(t *testing.T) {
	t.Parallel()
	html := "<!doctype html><html><head><title>Calls</title></head>\n" +
		"<body><div id=\"a\"></div><script>\n" +
		"function refresh(){ document.getElementById('a').textContent = 'x'; }\n" +
		"refresh();\n" +
		"refreshData();\n" +
		"</script></body></html>"
	result := validateReport(t, html, ReportHTMLValidationHooks{})
	for _, want := range []string{
		`"valid": false`,
		`calls undefined function \"refreshData\"`,
		`(line 5)`,
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected %q in result: %s", want, result)
		}
	}
	if strings.Contains(result, "calls undefined function \"refresh\"") {
		t.Fatalf("defined helper must not be reported: %s", result)
	}
	definedAcrossBlocks := `<!doctype html><html><head><title>Across</title></head><body>` +
		`<script>function fmt(n){ return String(n); }</script>` +
		`<div id="a"></div>` +
		`<script>document.getElementById('a').textContent = fmt(42);</script>` +
		`</body></html>`
	result = validateReport(t, definedAcrossBlocks, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": true`) {
		t.Fatalf("expected cross-block calls to resolve: %s", result)
	}
}

func TestValidateHTMLReportRejectsUnknownReportMethods(t *testing.T) {
	t.Parallel()
	html := `<!doctype html><html><head><title>API</title></head><body><script>
window.report.ready(async function(){
  var rows = await window.report.query('SELECT 1');
  var x = await window.report.fetchTable('t');
  var y = await report.getRows('t');
});
</script></body></html>`
	result := validateReport(t, html, ReportHTMLValidationHooks{})
	for _, want := range []string{
		`"valid": false`,
		`window.report.fetchTable does not exist`,
		`window.report.getRows does not exist`,
		`available methods`,
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected %q in result: %s", want, result)
		}
	}
	if strings.Contains(result, "window.report.query does not exist") {
		t.Fatalf("known method must not be reported: %s", result)
	}
}

func TestValidateHTMLReportChecksCallbackReferences(t *testing.T) {
	t.Parallel()
	html := `<!doctype html><html><head><title>Callbacks</title></head><body><script>
function boot(){}
window.report.ready(boot);
window.report.ready(boot2);
document.addEventListener('click', onDocClick);
setTimeout(later, 100);
fetch('/x').then(showRows);
</script></body></html>`
	result := validateReport(t, html, ReportHTMLValidationHooks{})
	for _, want := range []string{
		`"valid": false`,
		`passes undefined function \"boot2\" as a callback`,
		`passes undefined function \"onDocClick\" as a callback`,
		`passes undefined function \"later\" as a callback`,
		`passes undefined function \"showRows\" as a callback`,
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected %q in result: %s", want, result)
		}
	}
	if strings.Contains(result, "passes undefined function \"boot\"") {
		t.Fatalf("defined callback must not be reported: %s", result)
	}
}

func TestValidateHTMLReportWarnsWhenDarkModeOnlyFollowsTheOS(t *testing.T) {
	t.Parallel()
	osOnly := `<!doctype html><html><head><title>Theme</title><style>@media (prefers-color-scheme: dark){body{background:#000}}</style></head><body>x</body></html>`
	result := validateReport(t, osOnly, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": true`) || !strings.Contains(result, "ignores the app's light/dark toggle") {
		t.Fatalf("expected a theme warning without failing validation: %s", result)
	}
	appTheme := `<!doctype html><html><head><title>Theme</title><style>:root.dark body{background:#000}</style></head><body>x</body></html>`
	result = validateReport(t, appTheme, ReportHTMLValidationHooks{})
	if strings.Contains(result, "ignores the app's light/dark toggle") {
		t.Fatalf("a report keyed off .dark must not be warned: %s", result)
	}
}

// Condensed from real workflow reports (ICICI-BANK-PARSING, trading): inline
// onclick handlers, ready() with a DOMContentLoaded fallback, arrow helpers,
// regex replaces, and method calls. All of it must validate cleanly -- the
// new checks must not cry wolf on idiomatic reports.
func TestValidateHTMLReportAcceptsRealisticReportPatterns(t *testing.T) {
	t.Parallel()
	html := `<!doctype html><html><head><title>Bank</title><style>:root.dark body{color:#fff}</style></head>
<body>
<button class="nav-btn" onclick="switchTab('balances', this)">Balances</button>
<div id="cards"></div><div id="activity-container"></div>
<script>
function switchTab(tabId, btn) {
  document.querySelectorAll('.tab-pane').forEach(function(p) { p.classList.remove('active'); });
  var target = document.getElementById('tab-' + tabId);
  if (target) target.classList.add('active');
  if (btn) btn.classList.add('active');
}
function esc(t) {
  if (t == null) return '';
  return String(t).replace(/&/g, '&amp;').replace(/</g, '&lt;');
}
const fINR = v => v === null ? 'x' : 'Rs ' + Number(v).toLocaleString('en-IN');
async function render() {
  var cards = document.getElementById('cards');
  if (!window.report || typeof window.report.ready !== 'function') return;
  var rows = await window.report.query('SELECT a, b FROM t');
  var html = rows.map(function(r){ return '<div>' + esc(r.a) + '</div>'; }).join('');
  cards.innerHTML = html;
  var tpl = '<ul>' + rows.map(function(r){ return rowItem(r); }).join('') + '</ul>';
  document.getElementById('activity-container').innerHTML = tpl;
}
function rowItem(r){ return '<li>' + esc(r.b) + '</li>'; }
if (window.report && typeof window.report.ready === 'function') {
  window.report.ready(render);
} else {
  window.addEventListener('DOMContentLoaded', render);
}
</script></body></html>`
	result := validateReport(t, html, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": true`) {
		t.Fatalf("expected the realistic report to validate: %s", result)
	}
	for _, unwanted := range []string{"undefined function", "before that element", "does not exist"} {
		if strings.Contains(result, unwanted) {
			t.Fatalf("unexpected %q in result: %s", unwanted, result)
		}
	}
}

func TestValidateHTMLReportRejectsModuleSyntaxAndExternalScripts(t *testing.T) {
	t.Parallel()
	html := `<!doctype html><html><head><title>Modules</title>
<script src="report.js"></script>
</head><body><div id="a"></div><script>
import { fmt } from 'helpers';
export const x = 1;
document.getElementById('a').textContent = fmt(x);
</script></body></html>`
	result := validateReport(t, html, ReportHTMLValidationHooks{})
	for _, want := range []string{
		`"valid": false`,
		`will not load (line 2)`,
		`ES module import found`,
		`ES module export found`,
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected %q in result: %s", want, result)
		}
	}
}

func TestValidateHTMLReportRejectsContentAfterHtmlAndBase(t *testing.T) {
	t.Parallel()
	trailing := `<!doctype html><html><head><title>Trailing</title></head><body><div id="a"></div><script>function late(){ document.getElementById('a').textContent = 'x'; } late();</script></body></html><script>late();</script>`
	result := validateReport(t, trailing, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": false`) || !strings.Contains(result, "is ignored by the browser (line 1)") {
		t.Fatalf("expected an after-</html> error: %s", result)
	}
	withBase := `<!doctype html><html><head><title>Base</title><base href="https://cdn.example.com/"></head><body>x</body></html>`
	result = validateReport(t, withBase, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": false`) || !strings.Contains(result, "repoints relative URLs") {
		t.Fatalf("expected a <base> error: %s", result)
	}
}

func TestValidateHTMLReportChecksLinkTags(t *testing.T) {
	t.Parallel()
	stylesheet := `<!doctype html><html><head><title>Links</title><link rel="stylesheet" href="app.css"></head><body>x</body></html>`
	result := validateReport(t, stylesheet, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": false`) || !strings.Contains(result, `will not resolve (line 1)`) {
		t.Fatalf("expected a stylesheet error: %s", result)
	}
	icon := `<!doctype html><html><head><title>Icon</title><link rel="icon" href="icon.png"></head><body>x</body></html>`
	result = validateReport(t, icon, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": true`) || !strings.Contains(result, `is ignored inside the sandboxed report (line 1)`) {
		t.Fatalf("expected an icon warning without failing validation: %s", result)
	}
}

func TestValidateHTMLReportRejectsUnclosedAndUnterminatedScripts(t *testing.T) {
	t.Parallel()
	unclosed := `<!doctype html><html><head><title>Unclosed</title></head><body><script>var a = 1;`
	result := validateReport(t, unclosed, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": false`) || !strings.Contains(result, "is never closed with") {
		t.Fatalf("expected an unclosed-script error: %s", result)
	}
	// The literal </script> inside the string ends the block early, leaving
	// the string unterminated.
	truncated := `<!doctype html><html><head><title>Truncated</title></head><body><script>var s = 'abc</script></body></html>`
	result = validateReport(t, truncated, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": false`) || !strings.Contains(result, "ends inside a string or comment") {
		t.Fatalf("expected an unterminated-string error: %s", result)
	}
}

func TestValidateHTMLReportIgnoresCodeLikeTextInStrings(t *testing.T) {
	t.Parallel()
	html := "<!doctype html><html><head><title>Strings</title></head><body><div id=\"a\"></div><script>\n" +
		"var sample = \"document.getElementById('ghost').innerHTML = 'x'; callGhost();\";\n" +
		"// callComment();\n" +
		"function real(){ document.getElementById('a').textContent = sample; }\n" +
		"real();\n" +
		"</script></body></html>"
	result := validateReport(t, html, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": true`) {
		t.Fatalf("expected code-like strings to validate: %s", result)
	}
	for _, unwanted := range []string{"ghost", "callGhost", "callComment"} {
		if strings.Contains(result, unwanted) {
			t.Fatalf("unexpected %q in result: %s", unwanted, result)
		}
	}
}

func TestValidateHTMLReportChecksTemplateInterpolations(t *testing.T) {
	t.Parallel()
	html := "<!doctype html><html><head><title>Interp</title></head><body><div id=\"a\"></div><script>\n" +
		"function goodRow(r){ return r; }\n" +
		"function render(rows){\n" +
		"  var t = \x60<ul>${rows.map(badRow).join('')}</ul>\x60;\n" +
		"  var u = \x60<ul>${rows.map(goodRow).join('')}</ul>\x60;\n" +
		"  var v = \x60<b>${formatCell(rows)}</b>\x60;\n" +
		"  document.getElementById('a').innerHTML = t + u + v;\n" +
		"}\n" +
		"window.report.ready(function(){ render([]); });\n" +
		"</script></body></html>"
	result := validateReport(t, html, ReportHTMLValidationHooks{})
	for _, want := range []string{
		`"valid": false`,
		`passes undefined function \"badRow\" as a callback`,
		`calls undefined function \"formatCell\"`,
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected %q in result: %s", want, result)
		}
	}
	if strings.Contains(result, "goodRow") {
		t.Fatalf("defined interpolation helper must not be reported: %s", result)
	}
}

// Both cases come from real reports: renderMarkdown is injected by the host
// runtime, and `async (...) =>` arrows must not read as calls to "async".
func TestValidateHTMLReportAcceptsHostMarkdownAndAsyncArrows(t *testing.T) {
	t.Parallel()
	html := `<!doctype html><html><head><title>Host</title></head><body><div id="a"></div><script>
function renderMarkdownHtml(md) {
  if (window.report && window.report.renderMarkdown) {
    return window.report.renderMarkdown(md);
  }
  return String(md);
}
async function load(rows) {
  const checks = rows.map(async ([label, path]) => {
    return label + path;
  });
  const done = await Promise.all(checks);
  document.getElementById('a').textContent = renderMarkdownHtml(done.join(','));
}
window.report.ready(function(){ load([]); });
</script></body></html>`
	result := validateReport(t, html, ReportHTMLValidationHooks{})
	if !strings.Contains(result, `"valid": true`) {
		t.Fatalf("expected host methods and async arrows to validate: %s", result)
	}
	for _, unwanted := range []string{"undefined function", "does not exist"} {
		if strings.Contains(result, unwanted) {
			t.Fatalf("unexpected %q in result: %s", unwanted, result)
		}
	}
}

func TestStripReportJSLengthPreserved(t *testing.T) {
	t.Parallel()
	cases := []string{
		`var s = "a'b"; // comment`,
		"var t = 'it\\'s';",
		"var u = `hello ${name} and ${f(x)}`;",
		`var re = /item(\d+)/g; var q = a / b / c;`,
		`return /ab+c/.test(s);`,
		"/* block */ x = 1; <!-- html comment\n--> tail();",
		"var nested = `a${`b${c}`}d`;",
		`el.replace(/'/g, '&#39;');`,
		"var div = a / b / c; var r = x % y;",
	}
	for _, code := range cases {
		stripped, _ := stripReportJS(code)
		if len(stripped) != len(code) {
			t.Fatalf("length changed for %q: %d -> %d (%q)", code, len(code), len(stripped), stripped)
		}
	}
}

func TestStripReportJSKeepsInterpolationsAndBlanksLiterals(t *testing.T) {
	t.Parallel()
	stripped, _ := stripReportJS("var t = `<ul>${rows.map(rowHtml).join('')}</ul>`;")
	if !strings.Contains(stripped, "rows.map(rowHtml)") {
		t.Fatalf("interpolation lost: %q", stripped)
	}
	if strings.Contains(stripped, "<ul>") {
		t.Fatalf("literal text survived stripping: %q", stripped)
	}
	stripped, _ = stripReportJS(`var re = /item(\d+)/g;`)
	if strings.Contains(stripped, "item") {
		t.Fatalf("regex literal survived stripping: %q", stripped)
	}
	stripped, _ = stripReportJS(`var q = a / b;`)
	if !strings.Contains(stripped, "/ b") {
		t.Fatalf("division must survive stripping: %q", stripped)
	}
}
