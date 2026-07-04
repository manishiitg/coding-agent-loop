package server

import (
	"fmt"
	"strings"
)

type workflowVersionUpgrade struct {
	from  string
	to    string
	label string
	query string
}

var workflowVersionUpgrades = []workflowVersionUpgrade{
	{
		from:  workflowContractInitialVersion,
		to:    "1.0.1",
		label: "upgrade-1.0.1",
		query: `WORKFLOW VERSION UPGRADE v1.0.0 -> v1.0.1.

This is a product-managed Pulse pre-step. Do ONLY this upgrade check, then stop and wait for the normal Pulse triage step.

1. Read workflow.json and builder/improve.html. Treat a missing workflow.json "version" as "1.0.0".
2. Call get_reference_doc(kind="review-improve-log") and get_reference_doc(kind="post-run-monitor"). If builder/improve.html uses the old narrow Pulse layout, update it to the current responsive workflow Pulse contract: viewport meta, no horizontal overflow, mobile-first/wide-safe cards, metadata under titles on narrow widths, overflow-wrap for long run notes, and compact latest-run/cost/time sections.
3. If workflow.json.publish or publish/status.json shows private/password/passphrase/secret_name/password-protected publishing, call get_reference_doc(kind="publish-strategy") and refresh the workflow's publish instructions/config notes to the current password-protected static publish contract: named secret only, never plaintext, encrypt baked HTML with StatiCrypt after staging, use one shared salt/remember flow so the viewer unlocks once, apply the Runloop dark password-gate styling instead of the default StatiCrypt page, and record only visibility + secret_name/status. If publish is not enabled or not password/private, do nothing for publish.
4. Append one concise Pulse entry to builder/improve.html that says this workflow was upgraded from v1.0.0 to v1.0.1 and lists what was applied or skipped.
5. Only after the applicable checks/updates are complete, update workflow.json "version" to "1.0.1". Do not change schema_version. Do not run the workflow, do not alter schedules, and do not call notify_user in this step.

Report the files changed and any intentional no-op decisions, then stop.`,
	},
	{
		from:  "1.0.1",
		to:    "1.0.2",
		label: "upgrade-1.0.2",
		query: `WORKFLOW VERSION UPGRADE v1.0.1 -> v1.0.2.

This is a product-managed Pulse pre-step. Do ONLY this upgrade check, then stop and wait for the normal Pulse triage step.

1. Read workflow.json, publish/status.json if it exists, and builder/improve.html.
2. If workflow.json.publish or publish/status.json shows private/password/passphrase/secret_name/password-protected publishing, call get_reference_doc(kind="publish-strategy") and refresh the workflow's publish instructions/config notes to the current Runloop dark password-gate contract: named secret only, never plaintext, encrypt baked HTML with StatiCrypt after staging, use one shared salt/remember flow so the viewer unlocks once, apply the Runloop dark password-gate styling instead of the default green/white StatiCrypt page, and record only visibility + secret_name/status. Do not change the destination URL, do not expose the password, and do not do the deploy in this upgrade step; the normal verified publish turn will republish with the new gate.
3. If publish is not enabled or not password/private, make no publish changes.
4. Append one concise Pulse entry to builder/improve.html that says this workflow was upgraded from v1.0.1 to v1.0.2 and whether private publish styling was applied or skipped.
5. Only after the applicable checks/updates are complete, update workflow.json "version" to "1.0.2". Do not change schema_version. Do not run the workflow, do not alter schedules, and do not call notify_user in this step.

Report the files changed and any intentional no-op decisions, then stop.`,
	},
	{
		from:  "1.0.2",
		to:    WorkflowContractCurrentVersion,
		label: "upgrade-1.0.3",
		query: `WORKFLOW VERSION UPGRADE v1.0.2 -> v1.0.3.

This is a product-managed Pulse pre-step. Do ONLY this report-dashboard upgrade check, then stop and wait for the normal Pulse triage step.

Goal: move old report dashboards to the current HTML-only contract. The React report viewer should only receive a lightweight navigation plan; report intelligence, layout, tables, charts, summaries, and recommendations belong in HTML documents under db/reports/.

1. Read workflow.json, reports/report_plan.json if it exists, builder/improve.html if it exists, and list db/reports/. Treat a missing workflow.json "version" as "1.0.0".
2. Inspect reports/report_plan.json for legacy dashboard widgets. Legacy means any widget kind other than "file" or "file-list", any widget that relies on old fields such as db/sql/format/chart/stat/table/cards/alert/text, or any dashboard content that is not registered as a file widget with renderFormat "html".
3. If legacy widgets exist, create or update durable HTML report document(s) under db/reports/. Preserve the user's current report intent, section headings, ordering, tabs, tables, charts, stats, alerts, and narrative decisions, but implement them inside the HTML using window.report.query(sql), window.report.get(path), and window.report.fileUrl(path) against durable db/, knowledgebase/, docs/, or report assets. Do not bake current query results as static text.
4. If a legacy widget has missing or empty source/db/sql fields, inspect db/, db/README.md, knowledgebase/, existing db/reports/*.html, and recent durable report artifacts to find the matching data. If the data mapping is genuinely unclear, keep the section visible in the HTML with a clear "Needs data mapping" state instead of silently deleting it.
5. Update reports/report_plan.json to be navigation only: version 1, sections with stable id/heading/layout, and entries that register the HTML documents using kind "file", renderFormat "html", and source like "db/reports/<report>.html". Remove legacy widget kinds such as table, chart, stat, cards, alert, text, markdown, and any old widget-only config.
6. File-list widgets may remain only for supporting evidence galleries or artifact folders. They must not be the primary dashboard. If a file-list is being used as the report itself, replace it with an HTML document that links or previews the evidence intentionally.
7. Validate reports/report_plan.json with validate_report_plan if the tool is available. Also sanity-check that every registered HTML source exists and is under db/reports/.
8. Append one concise Pulse entry to builder/improve.html, if that file exists, that says this workflow was upgraded from v1.0.2 to v1.0.3 and lists which report sections were migrated or why the migration was a no-op.
9. Only after the applicable checks/updates are complete, update workflow.json "version" to "1.0.3". Do not change schema_version. Do not run the workflow, do not alter schedules, and do not call notify_user in this step.

Report the files changed, legacy widgets removed, HTML reports created or updated, validation result, and any intentional no-op decisions, then stop.`,
	},
}

func workflowContractVersionForUpgrade(manifest *WorkflowManifest) string {
	if manifest == nil {
		return workflowContractInitialVersion
	}
	version := strings.TrimSpace(manifest.Version)
	if version == "" {
		return workflowContractInitialVersion
	}
	return version
}

func workflowVersionUpgradePlan(manifest *WorkflowManifest) []workflowVersionUpgrade {
	version := workflowContractVersionForUpgrade(manifest)
	if version == WorkflowContractCurrentVersion {
		return nil
	}

	byFrom := make(map[string]workflowVersionUpgrade, len(workflowVersionUpgrades))
	for _, upgrade := range workflowVersionUpgrades {
		byFrom[upgrade.from] = upgrade
	}

	seen := map[string]bool{}
	var plan []workflowVersionUpgrade
	for version != WorkflowContractCurrentVersion {
		if seen[version] {
			return plan
		}
		seen[version] = true

		upgrade, ok := byFrom[version]
		if !ok {
			return plan
		}
		plan = append(plan, upgrade)
		version = upgrade.to
	}
	return plan
}

func postRunMonitorStepsForManifest(manifest *WorkflowManifest) []postRunMonitorStep {
	upgrades := workflowVersionUpgradePlan(manifest)
	base := postRunMonitorSteps()
	if len(upgrades) == 0 {
		return base
	}

	steps := make([]postRunMonitorStep, 0, len(upgrades)+len(base))
	for _, upgrade := range upgrades {
		steps = append(steps, postRunMonitorStep{
			label: upgrade.label,
			query: fmt.Sprintf(
				"%s\n\nCurrent workflow.json version seen by scheduler: %q. Target workflow contract version: %q.",
				upgrade.query,
				workflowContractVersionForUpgrade(manifest),
				WorkflowContractCurrentVersion,
			),
		})
	}
	steps = append(steps, base...)
	return steps
}
