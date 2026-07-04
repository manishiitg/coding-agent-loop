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
		to:    WorkflowContractCurrentVersion,
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
