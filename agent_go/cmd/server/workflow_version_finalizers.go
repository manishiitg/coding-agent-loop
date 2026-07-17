package server

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	pulseSchemaRootPattern = regexp.MustCompile(`(?i)<html\b[^>]*\bdata-pulse-schema\s*=\s*["']2["'][^>]*>`)
	pulseViewportPattern   = regexp.MustCompile(`(?i)<meta\b[^>]*\bname\s*=\s*["']viewport["'][^>]*>`)
	pulseHandoffPattern    = regexp.MustCompile(`(?i)\bid\s*=\s*["']pulse-agent-handoff["']`)
	pulseDatePickerPattern = regexp.MustCompile(`(?i)\bid\s*=\s*["']filter-date["']`)
	pulseDatedTagPattern   = regexp.MustCompile(`(?is)<(?:div|article|section)\b[^>]*\bdata-date\s*=\s*["']\d{4}-\d{2}-\d{2}["'][^>]*>`)
	pulseKindPattern       = regexp.MustCompile(`(?i)\bdata-kind\s*=\s*["'][^"']+["']`)
	pulseSectionPattern    = regexp.MustCompile(`(?i)\bdata-pulse-section\s*=\s*["'](?:signals|reflection|improvements)["']`)
	pulseModulePattern     = regexp.MustCompile(`(?i)\bdata-module\s*=\s*["'][^"']+["']`)
	pulseResponsiveWidth   = regexp.MustCompile(`(?i)(?:max-)?width\s*:\s*100%`)
	pulseOverflowGuard     = regexp.MustCompile(`(?i)overflow-x\s*:\s*hidden`)
	pulseHTMLComment       = regexp.MustCompile(`(?s)<!--.*?-->`)
)

func hasTrustedWorkflowUpgradeFinalizer(target string) bool {
	switch target {
	case workflowContractMessageSequenceCodeVersion, workflowContractPulseHistoryVersion:
		return true
	default:
		return false
	}
}

func finalizeTrustedWorkflowUpgrade(ctx context.Context, workspacePath, target string, manifest *WorkflowManifest) error {
	switch target {
	case workflowContractMessageSequenceCodeVersion:
		return finalizeMessageSequenceCodeUpgrade(ctx, workspacePath, manifest)
	case workflowContractPulseHistoryVersion:
		return finalizePulseHistoryContractUpgrade(ctx, workspacePath, manifest)
	default:
		return fmt.Errorf("workflow version %q has no trusted finalizer", target)
	}
}

func finalizePulseHistoryContractUpgrade(ctx context.Context, workspacePath string, manifest *WorkflowManifest) error {
	if manifest == nil {
		return errors.New("workflow manifest is missing")
	}
	if len(manifest.MalformedConfig) > 0 {
		return fmt.Errorf("workflow manifest has malformed config block(s) %v; refusing to rewrite it", manifest.MalformedConfig)
	}
	if workflowContractVersionForUpgrade(manifest) != workflowContractMessageSequenceCodeVersion {
		return fmt.Errorf(
			"expected workflow version %s before finalizing %s, found %q",
			workflowContractMessageSequenceCodeVersion,
			workflowContractPulseHistoryVersion,
			workflowContractVersionForUpgrade(manifest),
		)
	}

	content, exists, err := readFileFromWorkspace(ctx, strings.TrimSuffix(workspacePath, "/")+"/builder/improve.html")
	if err != nil {
		return fmt.Errorf("read builder/improve.html: %w", err)
	}
	if !exists {
		return errors.New("builder/improve.html is missing")
	}
	if err := validatePulseHistoryContract(content); err != nil {
		return err
	}

	manifest.Version = workflowContractPulseHistoryVersion
	if err := WriteWorkflowManifest(ctx, workspacePath, manifest); err != nil {
		return fmt.Errorf("stamp workflow version %s: %w", workflowContractPulseHistoryVersion, err)
	}
	return nil
}

func validatePulseHistoryContract(content string) error {
	var violations []string
	visibleContent := pulseHTMLComment.ReplaceAllString(content, "")
	if count := len(pulseSchemaRootPattern.FindAllString(content, -1)); count != 1 {
		violations = append(violations, fmt.Sprintf("expected exactly one schema-2 html root, found %d", count))
	}
	if count := len(pulseViewportPattern.FindAllString(content, -1)); count != 1 {
		violations = append(violations, fmt.Sprintf("expected exactly one viewport meta tag, found %d", count))
	}
	if count := strings.Count(content, "<!-- LOG ENTRIES: newest first -->"); count != 1 {
		violations = append(violations, fmt.Sprintf("expected exactly one newest-first log anchor, found %d", count))
	}
	if count := len(pulseHandoffPattern.FindAllString(content, -1)); count != 1 {
		violations = append(violations, fmt.Sprintf("expected exactly one pulse-agent-handoff block, found %d", count))
	}
	if pulseDatePickerPattern.MatchString(content) {
		violations = append(violations, "date picker filter-date is still present")
	}
	if !pulseResponsiveWidth.MatchString(content) || !pulseOverflowGuard.MatchString(content) {
		violations = append(violations, "responsive width or horizontal-overflow guard is missing")
	}
	if !strings.Contains(visibleContent, "v1.0.11") {
		violations = append(violations, "v1.0.11 migration entry is missing")
	}

	datedTags := pulseDatedTagPattern.FindAllString(visibleContent, -1)
	if len(datedTags) == 0 {
		violations = append(violations, "no real dated Pulse record was found")
	}
	for i, tag := range datedTags {
		if !pulseKindPattern.MatchString(tag) {
			violations = append(violations, fmt.Sprintf("dated record %d is missing data-kind", i+1))
		}
		if !pulseSectionPattern.MatchString(tag) {
			violations = append(violations, fmt.Sprintf("dated record %d has no canonical Pulse section", i+1))
		}
		if !pulseModulePattern.MatchString(tag) {
			violations = append(violations, fmt.Sprintf("dated record %d is missing data-module", i+1))
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("builder/improve.html does not satisfy the %s Pulse history contract: %s", workflowContractPulseHistoryVersion, strings.Join(violations, "; "))
	}
	return nil
}
