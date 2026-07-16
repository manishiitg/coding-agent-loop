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

This is a product-managed Pulse pre-step. Do ONLY this upgrade check, then stop and wait for the normal Pulse Gate step.

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

This is a product-managed Pulse pre-step. Do ONLY this upgrade check, then stop and wait for the normal Pulse Gate step.

1. Read workflow.json, publish/status.json if it exists, and builder/improve.html.
2. If workflow.json.publish or publish/status.json shows private/password/passphrase/secret_name/password-protected publishing, call get_reference_doc(kind="publish-strategy") and refresh the workflow's publish instructions/config notes to the current Runloop dark password-gate contract: named secret only, never plaintext, encrypt baked HTML with StatiCrypt after staging, use one shared salt/remember flow so the viewer unlocks once, apply the Runloop dark password-gate styling instead of the default green/white StatiCrypt page, and record only visibility + secret_name/status. Do not change the destination URL, do not expose the password, and do not do the deploy in this upgrade step; the normal verified publish turn will republish with the new gate.
3. If publish is not enabled or not password/private, make no publish changes.
4. Append one concise Pulse entry to builder/improve.html that says this workflow was upgraded from v1.0.1 to v1.0.2 and whether private publish styling was applied or skipped.
5. Only after the applicable checks/updates are complete, update workflow.json "version" to "1.0.2". Do not change schema_version. Do not run the workflow, do not alter schedules, and do not call notify_user in this step.

Report the files changed and any intentional no-op decisions, then stop.`,
	},
	{
		from:  "1.0.2",
		to:    "1.0.3",
		label: "upgrade-1.0.3",
		query: `WORKFLOW VERSION UPGRADE v1.0.2 -> v1.0.3.

This is a product-managed Pulse pre-step. Do ONLY this report-dashboard upgrade check, then stop and wait for the normal Pulse Gate step.

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
	{
		from:  "1.0.3",
		to:    "1.0.4",
		label: "upgrade-1.0.4",
		query: `WORKFLOW VERSION UPGRADE v1.0.3 -> v1.0.4.

This is a product-managed Pulse pre-step. Do ONLY this Pulse report readability upgrade, then stop and wait for the normal Pulse Gate step.

Goal: refresh workflow Pulse logs so builder/improve.html reads like a concise human/operator dashboard first, with the detailed evidence log below. This upgrade is for layout, readability, and stale-count cleanup only; do not change workflow behavior.

1. Read workflow.json, builder/improve.html if it exists, soul.md if it exists, recent run/cost/timing evidence if needed, and reports/report_plan.json only if it helps understand current report naming. Treat a missing workflow.json "version" as "1.0.0".
2. Call get_reference_doc(kind="review-improve-log") and update builder/improve.html to the current Pulse skeleton/CSS where needed:
   - first screen: two Bug/Goal verdict pills, one short status headline, chips, a "What matters now" brief, goal card, grouped signal tiles, and compact cost/time tiles;
   - recent runs: metadata row first, long prose/evidence note on a full-width second row, metadata/chips/timestamps no-wrap, prose/evidence fields wrap safely, no one-character metadata columns;
   - timeline: keep exactly one ` + "`<!-- LOG ENTRIES: newest first -->`" + ` anchor before the newest-first cards;
   - remove stale labels/counts only when evidence clearly supports the correction.
3. Preserve all existing unresolved findings, decisions, user rules, Chief of Staff recommendations, Artifact Review entries, recent-run evidence, and archive links. Do not delete evidence just because you are redesigning the shell.
4. If builder/improve.html is already on the current layout, make this a no-op except for appending the concise upgrade entry.
5. Append one concise Pulse entry to builder/improve.html that says this workflow was upgraded from v1.0.3 to v1.0.4 and lists what was applied or skipped.
6. Only after the applicable checks/updates are complete, update workflow.json "version" to "1.0.4". Do not change schema_version. Do not run the workflow, do not alter schedules, and do not call notify_user in this step.

	Report the files changed, Pulse sections refreshed, evidence preserved, and any intentional no-op decisions, then stop.`,
	},
	{
		from:  "1.0.4",
		to:    "1.0.5",
		label: "upgrade-1.0.5",
		query: `WORKFLOW VERSION UPGRADE v1.0.4 -> v1.0.5.

This is a product-managed Pulse pre-step. Do ONLY this filterability upgrade, then stop and wait for the normal Pulse Gate step.

Goal: make builder/improve.html searchable by activity kind and text so the user can inspect Pulse / Goal Advisor / Chief of Staff actions and notes without a separate date picker. Visible dates remain searchable as text.

1. Read workflow.json and builder/improve.html. Treat a missing workflow.json "version" as "1.0.0".
2. Call get_reference_doc(kind="review-improve-log") and update builder/improve.html to the current filterable Pulse skeleton where needed:
   - add the .filters bar with Kind, Search, Reset, and match count controls; do not add a date picker;
   - add the static filter script from the reference doc; this UI script is allowed and is not a legacy JSON data block;
   - add data-date="YYYY-MM-DD" and data-kind="run|monitor|artifact|decision|advisor|cos|open|user|note" to every recent-run row and timeline entry;
   - preserve exactly one <!-- LOG ENTRIES: newest first --> anchor before the newest-first timeline cards.
3. Backfill data dates/kinds from visible entry dates, run ids/folders, timestamp labels, tag text, or best available evidence. If a date is genuinely unknown, leave that specific old item unfiltered rather than fabricating a date, and mention it in the upgrade entry.
4. Preserve all existing unresolved findings, decisions, user rules, Chief of Staff recommendations, Artifact Review entries, run evidence, and archive links. Do not delete evidence while adding filter metadata.
5. Append one concise Pulse entry to builder/improve.html that says this workflow was upgraded from v1.0.4 to v1.0.5 and lists how many rows/cards were made filterable plus any skipped unknown-date items.
6. Only after the applicable checks/updates are complete, update workflow.json "version" to "1.0.5". Do not change schema_version. Do not run the workflow, do not alter schedules, and do not call notify_user in this step.

Report the files changed, filter metadata added, skipped unknown-date items if any, and any intentional no-op decisions, then stop.`,
	},
	{
		from:  "1.0.5",
		to:    "1.0.6",
		label: "upgrade-1.0.6",
		query: `WORKFLOW VERSION UPGRADE v1.0.5 -> v1.0.6.

This is a product-managed Pulse pre-step. Do ONLY this richer Pulse dashboard upgrade, then stop and wait for the normal Pulse Gate step.

Goal: make builder/improve.html more colorful, less text-heavy, and more widget-oriented so the user can understand workflow state quickly in the right panel.

1. Read workflow.json, builder/improve.html if it exists, soul.md if it exists, and recent run/cost/timing evidence only when needed to populate visible tiles. Treat a missing workflow.json "version" as "1.0.0".
2. Call get_reference_doc(kind="review-improve-log") and update builder/improve.html to the current rich widget Pulse shell where needed:
   - first screen has two Bug/Goal verdict pills, a one-sentence status banner, What matters now widget cards, a goal card, color-coded signal tiles, and cost/time tiles;
   - use .tile.ok, .tile.warn, .tile.bad, .tile.info, .tile.goal, and .tile.cost classes where the status is known;
   - replace dense first-screen prose/tables with compact widgets, chips, and card sections;
   - preserve the Kind/Search filter bar, data-date/data-kind attributes, and exactly one ` + "`<!-- LOG ENTRIES: newest first -->`" + ` anchor; do not add a date picker;
   - keep recent runs as readable mobile-first cards/rows with metadata first and long notes on a full-width second row.
3. Preserve all existing unresolved findings, decisions, user rules, Chief of Staff recommendations, Artifact Review entries, recent-run evidence, archive links, and filter metadata. Do not delete evidence just because you are redesigning the shell.
4. If builder/improve.html is already on the current rich widget layout, make this a no-op except for appending the concise upgrade entry.
5. Append one concise Pulse entry to builder/improve.html that says this workflow was upgraded from v1.0.5 to v1.0.6 and lists which visual sections were refreshed or skipped.
6. Only after the applicable checks/updates are complete, update workflow.json "version" to "1.0.6". Do not change schema_version. Do not run the workflow, do not alter schedules, and do not call notify_user in this step.

Report the files changed, widget sections refreshed, evidence preserved, and any intentional no-op decisions, then stop.`,
	},
	{
		from:  "1.0.6",
		to:    "1.0.7",
		label: "upgrade-1.0.7",
		query: `WORKFLOW VERSION UPGRADE v1.0.6 -> v1.0.7.

This is a product-managed Pulse pre-step. Do ONLY this legacy Auto Improve schedule cleanup, then stop and wait for the normal Pulse Gate step.

Goal: remove old separate Auto Improve / Goal Advisor optimizer schedules because Goal Advisor now runs as a Pulse-selected module after normal scheduled workflow runs.

1. Read workflow.json and builder/improve.html if it exists. Treat a missing workflow.json "version" as "1.0.0".
2. Inspect workflow.json "schedules". Identify legacy product Auto Improve / Goal Advisor schedules ONLY when:
   - workshop_mode is "optimizer"; AND
   - messages is missing/empty OR the messages match the old fixed product queue shape, such as STEP 1/5 PRE-BACKUP, STEP 2/5 IMPROVE or GOAL ADVISOR, later backup/publish/notify steps.
   Schedule name text such as "Auto Improve" or "Goal Advisor" is supporting evidence only. Do not remove a schedule by name alone.
3. Preserve explicit custom optimizer jobs: if workshop_mode="optimizer" has a real user-authored custom message with specific scope, evidence window, stop conditions, or non-product task intent, keep it unchanged.
4. For each legacy product optimizer schedule, remove it from workflow.json schedules. Do not delete or rewrite schedule-runs.json history; old run history stays as evidence.
5. If any legacy optimizer schedule was removed, set workflow.json post_run_monitor=true so the normal scheduled run can run Pulse Gate and select Goal Advisor when due. Do not create a new schedule here.
6. Append one concise Pulse entry to builder/improve.html, if that file exists, that says this workflow was upgraded from v1.0.6 to v1.0.7, lists removed legacy optimizer schedule ids/names, and lists preserved custom optimizer schedule ids/names if any.
7. Only after the applicable checks/updates are complete, update workflow.json "version" to "1.0.7". Do not change schema_version. Do not run the workflow, do not call notify_user, and do not publish in this step.

Report the files changed, legacy optimizer schedules removed, custom optimizer schedules preserved, post_run_monitor state, and any intentional no-op decisions, then stop.`,
	},
	{
		from:  "1.0.7",
		to:    "1.0.8",
		label: "upgrade-1.0.8",
		query: `WORKFLOW VERSION UPGRADE v1.0.7 -> v1.0.8.

This is a product-managed Pulse pre-step. Do ONLY this Pulse filter cleanup, then stop and wait for the normal Pulse Gate step.

Goal: remove the date picker from builder/improve.html while retaining useful activity filtering.

1. Read workflow.json and builder/improve.html if it exists.
2. Call get_reference_doc(kind="review-improve-log") and update builder/improve.html where needed:
   - remove the Date label and input (including id="filter-date") from the .filters bar;
   - remove dateInput and exact-date filtering logic from the static filter script;
   - keep Kind, Search, Reset, and match count controls working;
   - keep visible dates and data-date attributes as event metadata so dates remain readable and searchable through Search;
   - preserve data-kind attributes and exactly one <!-- LOG ENTRIES: newest first --> anchor.
3. Preserve all existing unresolved findings, decisions, user rules, human-input cards, recent-run evidence, timeline entries, and archive links. Do not rewrite or delete report history for this UI cleanup.
4. If builder/improve.html is missing, do not create it solely for this upgrade. If it already has no date picker, make this a no-op.
5. Only after the applicable check/update is complete, update workflow.json "version" to "1.0.8". Do not change schema_version. Do not run the workflow, alter schedules, notify the user, or publish in this step.

Report whether the date picker was removed or already absent, then stop.`,
	},
	{
		from:  "1.0.8",
		to:    "1.0.9",
		label: "upgrade-1.0.9",
		query: `WORKFLOW VERSION UPGRADE v1.0.8 -> v1.0.9.

This is a product-managed Pulse pre-step. Do ONLY this stable-soul and human-first Pulse upgrade, then stop and wait for the normal Pulse Gate step.

Goals:
- keep soul/soul.md limited to stable intent so old architecture and agent assumptions cannot freeze workflow evolution;
- make builder/improve.html readable in priority order: Needs your decision, Assumptions challenged, Today's outcome, goal progress, recent activity, then technical detail.

1. Read workflow.json, soul/soul.md, builder/improve.html if it exists, planning/plan.json, planning/step_config.json, and only the targeted changelog/user-rule evidence needed to distinguish explicit user-approved constraints from agent-inferred choices.
2. Normalize soul/soul.md:
   - keep ## Objective and ## Success Criteria;
   - keep ## Notifications when present;
   - keep an optional ## Constraints section only for boundaries explicitly stated or approved by the user;
   - remove architecture, step design, provider/tool/model choices, implementation details, historical decision logs, references, and agent-inferred assumptions from soul.md;
   - do not lose material context: when removed architecture is already represented by plan/config, cite those artifacts in the upgrade entry; when a consequential restriction is not clearly user-approved, preserve it as an active Assumptions challenged item in builder/improve.html with its source, evidence, and validation/retirement condition. Create a human-input request only when deciding whether it is a durable user constraint would materially change workflow behavior.
3. Call get_reference_doc(kind="review-improve-log") and get_reference_doc(kind="review-improve-log-skeleton"). Upgrade builder/improve.html in place when it exists:
   - replace the vague What matters now brief with Today's outcome cells: Outcome, Goal progress, Issues & fixes, Next Pulse;
   - add the optional Assumptions challenged block near the top, with at most three active consequential assumptions and no empty-state block;
   - wrap signal, cost/time, Maintenance Radar, cadence, and raw technical details in a closed-by-default <details class="technical"> section;
   - add one closed-by-default Agent log at the bottom with #pulse-agent-handoff for current module/cadence state, cursors, unresolved/pending ids, and evidence pointers; it must update in place and must not repeat the user-facing report;
   - preserve verdicts, goal criteria, pending/answered human-input history, unresolved findings, decisions, user rules, Chief of Staff recommendations, recent runs, timeline entries, filters, archive links, and exactly one <!-- LOG ENTRIES: newest first --> anchor;
   - do not duplicate the full pending question in the timeline; Runloop renders report_human_inputs first as Needs your decision.
4. If builder/improve.html is missing, do not create it solely for this upgrade. The next normal Pulse creates it with the current skeleton.
5. Add one concise upgrade timeline entry only when builder/improve.html exists. State what was removed from soul, which items remained explicit constraints, which assumptions were surfaced, and which Pulse sections changed. Do not expose secret values.
6. Only after the applicable checks/updates are complete, update workflow.json "version" to "1.0.9". Do not change schema_version. Do not run the workflow, alter schedules, notify the user, publish, or apply a strategy/plan change in this step.

Report the files changed, soul sections retained/removed, challenged assumptions surfaced, Pulse sections upgraded, and any intentional no-op decisions, then stop.`,
	},
	{
		from:  "1.0.9",
		to:    WorkflowContractCurrentVersion,
		label: "upgrade-1.0.10",
		query: `WORKFLOW VERSION UPGRADE v1.0.9 -> v1.0.10.

This is a product-managed workflow preflight. Do ONLY this message-sequence code migration, then stop and wait for the next preflight turn or the scheduled workflow message.

Goal: remove hidden Python execution from message_sequence. Deterministic code must be a visible standalone scripted workflow step with durable file or DB inputs and outputs.

1. Read workflow.json and planning/plan.json. Inventory every message_sequence item whose type is "code", including nested todo-task routes and orphan steps.
2. If there are no code items, make no plan change.
3. If code items exist, call migrate_message_sequence_code_items. This trusted migration tool converts only unambiguous top-level sequences made entirely of code items and their following prevalidation gates. It creates one standalone regular scripted step per code item, copies its source to learnings/<new-step-id>/main.py, carries forward cumulative file dependencies, attaches validation to the correct step, and creates scripted step_config entries.
4. Inspect the tool result. If it reports an ambiguous mixed conversational/code sequence, nested usage, unsupported input_json, or another blocker, do not guess and do not stamp the workflow version. Report the exact sequence/item and required explicit split. The scheduler will block the normal workflow run so the old plan cannot fail halfway through execution.
5. Re-read planning/plan.json and planning/step_config.json. Confirm:
   - no message_sequence item has type "code";
   - every migrated script exists at learnings/<step-id>/main.py;
   - every migrated regular step has declared_execution_mode="scripted";
   - durable context_dependencies/context_output and validation preserve the old sequence ordering and output contract.
6. Only after those checks pass, update workflow.json "version" to "1.0.10". Do not change schema_version. Do not run the workflow, notify the user, publish, or make unrelated plan improvements.

Report migrated sequence ids, new scripted step ids, copied script paths, validation/context handoffs, no-op decisions, and blockers, then stop.`,
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
	steps := postRunMonitorUpgradeStepsForManifest(manifest)
	steps = append(steps, postRunMonitorSteps()...)
	return steps
}

func postRunMonitorUpgradeStepsForManifest(manifest *WorkflowManifest) []postRunMonitorStep {
	upgrades := workflowVersionUpgradePlan(manifest)
	if len(upgrades) == 0 {
		return nil
	}

	steps := make([]postRunMonitorStep, 0, len(upgrades))
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
	return steps
}
