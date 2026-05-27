// Command migrate-mode-names rewrites persisted workflow configuration files
// to use the canonical mode names "agentic" and "scripted" in place of the
// legacy "code_exec" and "learn_code". Run once after deploying the rename;
// the application's read path also accepts the legacy names indefinitely via
// canonicalDeclaredExecutionMode, so this migration is a cleanup, not a hard
// requirement.
//
// Targets: planning/step_config.json, planning/output_plan.json,
// learnings/{step-id}/script_metadata.json under any workspace-docs/ root.
//
// Rewrites:
//   - JSON string values "code_exec" -> "agentic"
//   - JSON string values "learn_code" -> "scripted"
//   - JSON object keys "code_exec" -> "agentic" (e.g. in successful_runs map)
//   - JSON object keys "learn_code" -> "scripted"
//
// The rewrite is purely textual on string-delimited tokens; it does not
// touch substrings like "code_execution_mode" or "learn_code_max_fix_iterations"
// because those identifiers don't appear inside string-quoted JSON values
// — only as bare keys, which are also quoted but as full tokens that don't
// match "code_exec" or "learn_code" exactly.
//
// Usage:
//
//	go run ./cmd/migrate-mode-names <root>
//
// Where <root> is the workspace-docs directory (defaults to "./workspace-docs").
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	codeExecOld  = []byte(`"code_exec"`)
	codeExecNew  = []byte(`"agentic"`)
	learnCodeOld = []byte(`"learn_code"`)
	learnCodeNew = []byte(`"scripted"`)
)

var targetNames = map[string]bool{
	"step_config.json":     true,
	"output_plan.json":     true,
	"script_metadata.json": true,
}

// Legacy filename rename map (Phase 4): the saved fast-path execution log
// moved from learn_code_fast_path.json to scripted_fast_path.json. Old runs
// in workspace-docs/Workflow/.../runs/.../logs/.../execution/ keep the old
// name on disk; this migration renames them in place.
var legacyFilenameRenames = map[string]string{
	"learn_code_fast_path.json": "scripted_fast_path.json",
}

// Inside the fast-path file the "mode" field also stores the legacy
// "learn_code_fast_path" marker — rewrite to the canonical name on
// the way past.
var fastPathModeOld = []byte(`"mode": "learn_code_fast_path"`)
var fastPathModeNew = []byte(`"mode": "scripted_fast_path"`)

func main() {
	dryRun := flag.Bool("dry-run", false, "Show what would change without writing")
	flag.Parse()

	root := flag.Arg(0)
	if root == "" {
		root = "./workspace-docs"
	}
	root = filepath.Clean(root)

	info, err := os.Stat(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stat %s: %v\n", root, err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "%s is not a directory\n", root)
		os.Exit(1)
	}

	var rewritten, renamed, skipped int
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Phase 4: rename legacy fast-path filenames in place. Also rewrite
		// the "mode" field inside so the file's self-described marker stays
		// consistent with its filename.
		if newName, ok := legacyFilenameRenames[d.Name()]; ok {
			newPath := filepath.Join(filepath.Dir(path), newName)
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
				return nil
			}
			out := bytes.ReplaceAll(data, fastPathModeOld, fastPathModeNew)
			if *dryRun {
				fmt.Printf("WOULD RENAME: %s -> %s\n", path, newPath)
			} else {
				if err := os.WriteFile(newPath, out, 0o644); err != nil {
					fmt.Fprintf(os.Stderr, "write %s: %v\n", newPath, err)
					return nil
				}
				if err := os.Remove(path); err != nil {
					fmt.Fprintf(os.Stderr, "remove %s: %v\n", path, err)
					return nil
				}
				fmt.Printf("renamed: %s -> %s\n", path, newPath)
			}
			renamed++
			return nil
		}
		if !targetNames[d.Name()] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
			return nil
		}
		if !bytes.Contains(data, codeExecOld) && !bytes.Contains(data, learnCodeOld) {
			skipped++
			return nil
		}
		out := bytes.ReplaceAll(data, codeExecOld, codeExecNew)
		out = bytes.ReplaceAll(out, learnCodeOld, learnCodeNew)
		if bytes.Equal(out, data) {
			skipped++
			return nil
		}
		if *dryRun {
			fmt.Printf("WOULD REWRITE: %s\n", path)
		} else {
			if err := os.WriteFile(path, out, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
				return nil
			}
			fmt.Printf("rewrote: %s\n", path)
		}
		rewritten++
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk: %v\n", err)
		os.Exit(1)
	}
	verb := "rewrote"
	if *dryRun {
		verb = "would rewrite"
	}
	renameVerb := "renamed"
	if *dryRun {
		renameVerb = "would rename"
	}
	fmt.Printf("\nDone. %s %d file(s); %s %d legacy filename(s); %d eligible file(s) had no changes to apply.\n", verb, rewritten, renameVerb, renamed, skipped)
}
