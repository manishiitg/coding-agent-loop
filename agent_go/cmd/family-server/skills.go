package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// seededSkillsFS holds the app-provided SKILL.md files, embedded into the binary.
//
// "all:" is required, not just "skills": Go's embed silently EXCLUDES any file
// or directory whose name starts with "_" or "." unless the pattern has the
// "all:" prefix. skills/_shared/ (the shared design system every generated
// HTML file is supposed to inline) starts with "_" — without "all:" it was
// silently never embedded, so seedSkills() never wrote it and the agent
// correctly (if confusingly) reported the file as missing and improvised its
// own version instead.
//
//go:embed all:skills
var seededSkillsFS embed.FS

// seedSkills copies the embedded SKILL.md files into the family workspace under
// skills/, so the agent can read them on demand via the shell (e.g.
// `cat skills/create-test/SKILL.md`). Skills are app assets, so they are
// overwritten on every startup — skill updates ship with the binary. This never
// touches the parent/child/shared content folders.
func seedSkills() {
	base := filepath.Join(familyDataDir(), "workspace")
	_ = fs.WalkDir(seededSkillsFS, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, readErr := seededSkillsFS.ReadFile(p)
		if readErr != nil {
			return nil
		}
		dest := filepath.Join(base, filepath.FromSlash(p)) // workspace/skills/...
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o700); mkErr != nil {
			return nil
		}
		_ = os.WriteFile(dest, data, 0o600)
		return nil
	})
}
