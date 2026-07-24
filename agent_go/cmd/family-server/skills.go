package main

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
//
// ALSO mirrors the same files into .agents/skills/ — the workspace-relative
// path Codex CLI natively scans for skills at session launch (confirmed live:
// real files there show up, correctly parsed, in Codex's own skills_instructions
// listing alongside its built-in skills — a symlink there is silently ignored,
// which is why this writes real copies, not a link). This gives every skill
// real progressive disclosure (name+description always visible to the model;
// full SKILL.md body read only when Codex decides it's relevant) on top of
// the system prompt's own "read the matching skill file" instruction, instead
// of relying on that prompt instruction alone.
func seedSkills() {
	base := filepath.Join(familyDataDir(), "workspace")
	nativeSkillsDir := filepath.Join(base, ".agents", "skills")
	_ = fs.WalkDir(seededSkillsFS, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, readErr := seededSkillsFS.ReadFile(p)
		if readErr != nil {
			return nil
		}
		rel := filepath.FromSlash(strings.TrimPrefix(p, "skills/"))
		dest := filepath.Join(base, filepath.FromSlash(p)) // workspace/skills/...
		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o700); mkErr != nil {
			return nil
		}
		_ = os.WriteFile(dest, data, 0o600)

		nativeDest := filepath.Join(nativeSkillsDir, rel) // workspace/.agents/skills/...
		if mkErr := os.MkdirAll(filepath.Dir(nativeDest), 0o700); mkErr != nil {
			return nil
		}
		_ = os.WriteFile(nativeDest, data, 0o600)
		return nil
	})
}
