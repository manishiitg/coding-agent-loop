// gog-migrate moves an existing connection registry to gog-owned authentication.
// It does not uninstall gws or remove its independent host configuration.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

func main() {
	config := flag.String("config", "", "path to workspace-docs/config/gmail-config.json")
	apply := flag.Bool("apply", false, "import missing credentials, verify all accounts and save the migrated registry")
	retire := flag.Bool("retire-legacy", false, "after saving, remove verified duplicate AgentWorks credentials (requires the updated server)")
	flag.Parse()
	if err := run(*config, *apply, *retire); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, apply, retire bool) error {
	if path == "" {
		return fmt.Errorf("--config is required; without --apply this command only verifies gog access")
	}
	if retire && !apply {
		return fmt.Errorf("--retire-legacy requires --apply")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg services.GmailConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if !apply {
		if err := services.VerifyGmailConfigGog(ctx, &cfg); err != nil {
			return err
		}
		fmt.Printf("Verified gog authentication for %d connections; no configuration changed.\n", len(cfg.Connections))
		return nil
	}
	migrated, err := services.MigrateGmailConfigToGog(ctx, &cfg)
	if err != nil {
		return err
	}
	newData, err := json.MarshalIndent(migrated, "", "  ")
	if err != nil {
		return err
	}
	latest, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(latest, raw) {
		return fmt.Errorf("Gmail configuration changed during verification; retry migration")
	}
	// Preserve the old registry before atomically replacing it. Credentials are
	// retained until the new registry has reached disk successfully.
	backup, err := os.CreateTemp(filepath.Dir(path), "gmail-config.before-gog-*.json")
	if err != nil {
		return err
	}
	backupName := backup.Name()
	if _, err := backup.Write(raw); err != nil {
		backup.Close()
		return err
	}
	if err := backup.Sync(); err != nil {
		backup.Close()
		return err
	}
	if err := backup.Close(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gmail-config-gog-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(newData, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	fmt.Printf("Migrated %d connections to gog. Previous registry: %s\n", len(migrated.Connections), backupName)
	if retire {
		if err := services.RetireLegacyGmailCredentials(migrated); err != nil {
			return fmt.Errorf("registry migrated; legacy cleanup incomplete: %w", err)
		}
		fmt.Println("Removed verified duplicate AgentWorks credentials. gws remains installed.")
	}
	return nil
}
