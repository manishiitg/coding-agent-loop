package agentprofiles

import (
	"fmt"
	"io/fs"
	"strings"
	"text/template"
	"text/template/parse"
)

// LoadChatPrompt validates and composes trusted product files. User/runtime
// values are supplied only when executing the resulting template, never parsed.
func LoadChatPrompt(fsys fs.FS, source ChatPromptSource) (string, error) {
	paths := append([]string{source.File}, source.Includes...)
	seen := map[string]bool{}
	var parts []string
	for i, path := range paths {
		if !fs.ValidPath(path) || path == "." || seen[path] {
			return "", fmt.Errorf("invalid or duplicate chat prompt path %q", path)
		}
		seen[path] = true
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return "", fmt.Errorf("read chat prompt %q: %w", path, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return "", fmt.Errorf("empty chat prompt %q", path)
		}
		if i > 0 {
			included, err := template.New(path).Parse(string(data))
			if err != nil {
				return "", fmt.Errorf("parse chat include %q: %w", path, err)
			}
			if strings.TrimSpace(included.Tree.Root.String()) != "" {
				return "", fmt.Errorf("chat include %q must contain named template definitions only", path)
			}
		}
		parts = append(parts, string(data))
	}
	text := strings.Join(parts, "\n")
	tmpl, err := template.New("chat").Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("parse chat prompt: %w", err)
	}
	// Parse alone accepts calls to undefined templates; catch those at startup.
	var visit func(parse.Node) error
	visit = func(node parse.Node) error {
		switch n := node.(type) {
		case *parse.ListNode:
			if n != nil {
				for _, child := range n.Nodes {
					if err := visit(child); err != nil {
						return err
					}
				}
			}
		case *parse.TemplateNode:
			if tmpl.Lookup(n.Name) == nil {
				return fmt.Errorf("undefined chat template %q", n.Name)
			}
		case *parse.IfNode:
			if err := visit(n.List); err != nil {
				return err
			}
			return visit(n.ElseList)
		case *parse.RangeNode:
			if err := visit(n.List); err != nil {
				return err
			}
			return visit(n.ElseList)
		case *parse.WithNode:
			if err := visit(n.List); err != nil {
				return err
			}
			return visit(n.ElseList)
		}
		return nil
	}
	for _, item := range tmpl.Templates() {
		if err := visit(item.Tree.Root); err != nil {
			return "", err
		}
	}
	return text, nil
}
