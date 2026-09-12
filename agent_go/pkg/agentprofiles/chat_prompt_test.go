package agentprofiles

import (
	"strings"
	"testing"
	"testing/fstest"
	"text/template"
)

func TestChatPromptComposition(t *testing.T) {
	fsys := fstest.MapFS{
		"builder.md": {Data: []byte(`{{template "shared" .}} Builder {{.Goal}}`)},
		"shared.md":  {Data: []byte(`{{define "shared"}}Shared rules.{{end}}`)},
	}
	source := ChatPromptSource{File: "builder.md", Includes: []string{"shared.md"}}
	text, err := LoadChatPrompt(fsys, source)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := template.Must(template.New("chat").Option("missingkey=error").Parse(text))
	var out strings.Builder
	if err := tmpl.Execute(&out, map[string]string{"Goal": `literal {{.Secret}}`}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != `Shared rules. Builder literal {{.Secret}}` {
		t.Fatal(out.String())
	}
	if err := tmpl.Execute(&out, map[string]string{}); err == nil {
		t.Fatal("missing runtime value silently accepted")
	}
	for _, tc := range []struct{ name, file, include, body string }{
		{"missing entry", "absent.md", "shared.md", ""},
		{"missing include", "builder.md", "absent.md", ""},
		{"traversal", "../builder.md", "shared.md", ""},
		{"absolute", "/builder.md", "shared.md", ""},
		{"duplicate", "builder.md", "builder.md", ""},
		{"syntax", "bad.md", "shared.md", "{{if}}"},
		{"missing definition", "bad.md", "shared.md", `{{if .Enabled}}{{template "absent" .}}{{end}}`},
		{"empty", "bad.md", "shared.md", " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys["bad.md"] = &fstest.MapFile{Data: []byte(tc.body)}
			if _, err := LoadChatPrompt(fsys, ChatPromptSource{File: tc.file, Includes: []string{tc.include}}); err == nil {
				t.Fatal("invalid prompt accepted")
			}
		})
	}
}

func TestManifestValidatesChatFiles(t *testing.T) {
	_, err := LoadProductManifest(manifestFS("chat:\n  builder:\n    prompt:\n      file: missing.md\n    skills: [system-tools]\n", nil), "product.yaml")
	if err == nil || !strings.Contains(err.Error(), "chat builder") {
		t.Fatalf("got %v", err)
	}
}
