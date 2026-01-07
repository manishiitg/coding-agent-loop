package step_based_workflow

import (
	"fmt"
	"strings"
	"sync"
	"text/template"
)

// TemplateRegistry holds all pre-parsed templates for the package.
// Templates are registered at package init time and parsed immediately,
// causing a panic at startup if any template has syntax errors.
type TemplateRegistry struct {
	templates map[string]*template.Template
	mu        sync.RWMutex
}

// globalRegistry is the singleton template registry
var globalRegistry = &TemplateRegistry{
	templates: make(map[string]*template.Template),
}

// MustRegisterTemplate registers a template and panics if parsing fails.
// This should be called from init() or package-level var declarations.
func MustRegisterTemplate(name, templateStr string) *template.Template {
	tmpl, err := template.New(name).Parse(templateStr)
	if err != nil {
		panic(fmt.Sprintf("failed to parse template %q: %v", name, err))
	}
	globalRegistry.mu.Lock()
	globalRegistry.templates[name] = tmpl
	globalRegistry.mu.Unlock()
	return tmpl
}

// GetTemplate retrieves a pre-parsed template by name.
// Returns nil if the template doesn't exist.
func GetTemplate(name string) *template.Template {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	return globalRegistry.templates[name]
}

// ExecuteTemplate executes a registered template with the given data.
// Returns an error if the template doesn't exist or execution fails.
func ExecuteTemplate(name string, data interface{}) (string, error) {
	tmpl := GetTemplate(name)
	if tmpl == nil {
		return "", fmt.Errorf("template %q not found in registry", name)
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		return "", fmt.Errorf("failed to execute template %q: %w", name, err)
	}
	return result.String(), nil
}

// MustExecuteTemplate executes a registered template and panics on error.
// Use this only when template execution failure should be fatal.
func MustExecuteTemplate(name string, data interface{}) string {
	result, err := ExecuteTemplate(name, data)
	if err != nil {
		panic(err)
	}
	return result
}

// ListTemplates returns all registered template names (for debugging)
func ListTemplates() []string {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	names := make([]string, 0, len(globalRegistry.templates))
	for name := range globalRegistry.templates {
		names = append(names, name)
	}
	return names
}
