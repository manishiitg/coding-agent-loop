// Package uiuxpromax embeds the upstream UI/UX Pro Max skill as an optional,
// progressively disclosed design reference shared by AgentWorks products.
package uiuxpromax

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	workspaceskills "github.com/manishiitg/coding-agent-loop/agent_go/pkg/skills"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

const (
	skillName     = "ui-ux-pro-max"
	skillRoot     = "bundle/ui-ux-pro-max"
	skillFilePath = skillRoot + "/SKILL.md"
	sourceURL     = "https://github.com/nextlevelbuilder/ui-ux-pro-max-skill"
)

//go:embed bundle/ui-ux-pro-max
var bundle embed.FS

var registerOnce sync.Once
var registerErr error

// Materialize returns a fresh skill bundle so callers can attach it directly
// without sharing mutable slices across retained sessions.
func Materialize() (*llmtypes.Skill, error) {
	data, err := fs.ReadFile(bundle, skillFilePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", skillFilePath, err)
	}
	frontmatter, body, err := workspaceskills.ParseSkillFile(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", skillFilePath, err)
	}
	if strings.TrimSpace(frontmatter.Name) != skillName {
		return nil, fmt.Errorf("embedded skill name = %q, want %q", frontmatter.Name, skillName)
	}

	var supporting []llmtypes.SkillFile
	err = fs.WalkDir(bundle, skillRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filePath == skillFilePath {
			return nil
		}
		contents, readErr := fs.ReadFile(bundle, filePath)
		if readErr != nil {
			return readErr
		}
		relPath := strings.TrimPrefix(filePath, skillRoot+"/")
		supporting = append(supporting, llmtypes.SkillFile{RelPath: path.Clean(relPath), Content: contents})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read %s bundle: %w", skillName, err)
	}

	return &llmtypes.Skill{
		Name:            skillName,
		Description:     strings.TrimSpace(frontmatter.Description),
		Content:         body,
		SupportingFiles: supporting,
		Source: llmtypes.SkillSource{
			Origin:    "builtin",
			SourceURL: sourceURL,
		},
		Metadata: map[string]string{
			"upstream_commit": "15de38fb70bc80ae9276fa7703b48ae861a672e6",
			"license":         "MIT",
		},
	}, nil
}

// Register exposes the bundle to the normal name-based skill loader. Multiple
// products call this during startup, so registration is process-wide once.
func Register() error {
	registerOnce.Do(func() {
		skill, err := Materialize()
		if err != nil {
			registerErr = err
			return
		}
		registerErr = workspaceskills.RegisterBuiltin(skill)
	})
	return registerErr
}
