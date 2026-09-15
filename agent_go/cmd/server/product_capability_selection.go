package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func projectRuntimeManifestPath(profileID, workspacePath string) string {
	name := "product.json"
	if strings.EqualFold(strings.TrimSpace(profileID), "work") {
		name = "workflow.json"
	}
	return filepath.ToSlash(filepath.Join(workspacePath, name))
}

func readProjectRuntimeManifest(ctx context.Context, profileID, workspacePath string) (string, bool, error) {
	raw, found, err := readFileFromWorkspace(ctx, projectRuntimeManifestPath(profileID, workspacePath))
	if err != nil || found || !strings.EqualFold(strings.TrimSpace(profileID), "work") {
		return raw, found, err
	}
	return readFileFromWorkspace(ctx, filepath.ToSlash(filepath.Join(workspacePath, "product.json")))
}

func workRuntimeManifestFromLegacy(raw string) (map[string]interface{}, error) {
	var legacy map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		return nil, err
	}
	manifest := map[string]interface{}{
		"schema_version": 1,
		"id":             legacy["id"],
		"label":          legacy["title"],
		"capabilities":   map[string]interface{}{},
		"schedules":      []interface{}{},
		"triggers":       []interface{}{},
		"created_at":     legacy["created_at"],
		"updated_at":     legacy["updated_at"],
	}
	for _, key := range []string{"capabilities", "schedules", "triggers"} {
		if value, ok := legacy[key]; ok {
			manifest[key] = value
		}
	}
	if value, ok := legacy["workflow_context_paths"]; ok {
		manifest["workflow_context_paths"] = value
	} else if capabilities, ok := legacy["capabilities"].(map[string]interface{}); ok {
		if value, ok := capabilities["workflow_context_paths"]; ok {
			manifest["workflow_context_paths"] = value
			delete(capabilities, "workflow_context_paths")
		}
	}
	capabilities, _ := manifest["capabilities"].(map[string]interface{})
	if capabilities == nil {
		capabilities = map[string]interface{}{}
		manifest["capabilities"] = capabilities
	}
	defaults := map[string]interface{}{
		"selected_tools":               []interface{}{},
		"selected_global_secret_names": []interface{}{},
		"browser_mode":                 "auto",
		"use_code_execution_mode":      false,
	}
	for key, value := range defaults {
		if _, exists := capabilities[key]; !exists {
			capabilities[key] = value
		}
	}
	return manifest, nil
}

func ensureProjectRuntimeManifest(ctx context.Context, profileID, workspacePath string) (string, string, error) {
	manifestPath := projectRuntimeManifestPath(profileID, workspacePath)
	raw, found, err := readFileFromWorkspace(ctx, manifestPath)
	if err != nil {
		return "", manifestPath, err
	}
	if found {
		return raw, manifestPath, nil
	}
	if !strings.EqualFold(strings.TrimSpace(profileID), "work") {
		return "", manifestPath, fmt.Errorf("product manifest not found")
	}
	legacyRaw, legacyFound, err := readFileFromWorkspace(ctx, filepath.ToSlash(filepath.Join(workspacePath, "product.json")))
	if err != nil || !legacyFound {
		if err == nil {
			err = fmt.Errorf("product manifest not found")
		}
		return "", manifestPath, err
	}
	manifest, err := workRuntimeManifestFromLegacy(legacyRaw)
	if err != nil {
		return "", manifestPath, fmt.Errorf("decode legacy product manifest: %w", err)
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", manifestPath, err
	}
	raw = string(encoded) + "\n"
	if err := writeFileToWorkspace(ctx, manifestPath, raw); err != nil {
		return "", manifestPath, err
	}
	// Strip the runtime fields from product.json only after workflow.json is
	// durable. Failure here is safe: workflow.json is already authoritative and
	// the remaining legacy fields are ignored.
	var metadata map[string]interface{}
	if json.Unmarshal([]byte(legacyRaw), &metadata) == nil {
		for _, key := range []string{"capabilities", "schedules", "triggers", "workflow_context_paths"} {
			delete(metadata, key)
		}
		metadata["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		if encodedMetadata, encodeErr := json.MarshalIndent(metadata, "", "  "); encodeErr == nil {
			_ = writeFileToWorkspace(ctx, filepath.ToSlash(filepath.Join(workspacePath, "product.json")), string(encodedMetadata)+"\n")
		}
	}
	return raw, manifestPath, nil
}

// productSelectedSecrets reads capabilities.selected_secrets from the product's
// canonical runtime manifest. Work uses workflow.json; legacy Work projects
// fall back to product.json until the first mutation creates workflow.json.
func productSelectedSecrets(ctx context.Context, profileID, workspacePath string) ([]string, bool, error) {
	raw, found, err := readProjectRuntimeManifest(ctx, profileID, workspacePath)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("product manifest not found")
		}
		return nil, false, err
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return nil, false, fmt.Errorf("decode product manifest: %w", err)
	}
	capabilities, _ := manifest["capabilities"].(map[string]interface{})
	value, initialized := capabilities["selected_secrets"]
	if !initialized {
		return nil, false, nil
	}
	items, ok := value.([]interface{})
	if !ok {
		return nil, true, fmt.Errorf("product capabilities.selected_secrets must be an array")
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item.(string)
		if ok {
			names = appendUniqueStrings(names, strings.TrimSpace(name))
		}
	}
	sort.Strings(names)
	return names, true, nil
}

func updateProductSelectedSecrets(ctx context.Context, profileID, workspacePath string, mutate func([]string) []string) error {
	raw, manifestPath, err := ensureProjectRuntimeManifest(ctx, profileID, workspacePath)
	if err != nil {
		return err
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return fmt.Errorf("decode product manifest: %w", err)
	}
	capabilities, _ := manifest["capabilities"].(map[string]interface{})
	if capabilities == nil {
		capabilities = map[string]interface{}{}
	}
	current := []string{}
	if items, ok := capabilities["selected_secrets"].([]interface{}); ok {
		for _, item := range items {
			if name, ok := item.(string); ok {
				current = appendUniqueStrings(current, strings.TrimSpace(name))
			}
		}
	}
	next := mutate(current)
	canonical := make([]string, 0, len(next))
	for _, name := range next {
		canonical = appendUniqueStrings(canonical, strings.TrimSpace(name))
	}
	sort.Strings(canonical)
	capabilities["selected_secrets"] = canonical
	manifest["capabilities"] = capabilities
	manifest["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode product manifest: %w", err)
	}
	return writeFileToWorkspace(ctx, manifestPath, string(encoded)+"\n")
}
