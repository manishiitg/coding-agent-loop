package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Releases own the base catalog; the service state directory owns user entries.
// Keep both files together because every MCP consumer derives the overlay from
// the base path. Local checkouts retain their existing paths unless configured.
func prepareMCPRuntimeConfig(source, stateDir string) (string, error) {
	if strings.TrimSpace(stateDir) == "" {
		return source, nil
	}
	base, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read MCP catalog: %w", err)
	}
	if !json.Valid(base) {
		return "", fmt.Errorf("invalid MCP catalog JSON")
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return "", err
	}
	dest := filepath.Join(stateDir, filepath.Base(source))
	overlay := strings.TrimSuffix(dest, ".json") + "_user.json"
	legacy := strings.TrimSuffix(source, ".json") + "_user.json"
	if _, err := os.Stat(overlay); os.IsNotExist(err) {
		data, readErr := os.ReadFile(legacy)
		if readErr == nil {
			if !json.Valid(data) {
				return "", fmt.Errorf("invalid legacy MCP overlay JSON")
			}
			// Publish only a complete file. Hard-link creation is exclusive, so
			// a concurrent start cannot replace an existing durable overlay.
			f, createErr := os.CreateTemp(stateDir, ".mcp-overlay-*")
			if createErr != nil {
				return "", createErr
			}
			tempName := f.Name()
			defer os.Remove(tempName)
			if _, err := f.Write(data); err != nil {
				f.Close()
				return "", err
			}
			if err := f.Close(); err != nil {
				return "", err
			}
			if err := os.Link(tempName, overlay); err != nil && !os.IsExist(err) {
				return "", err
			}

		} else if !os.IsNotExist(readErr) {
			return "", readErr
		}
	} else if err != nil {
		return "", err
	}
	// Atomic base refresh; a new release can add catalog entries without changing
	// the durable user overlay. Restrictive permissions also cover legacy secrets.
	f, err := os.CreateTemp(stateDir, ".mcp-catalog-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(base); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(name, dest); err != nil {
		return "", err
	}
	return dest, nil
}
