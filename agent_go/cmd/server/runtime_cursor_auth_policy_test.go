package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Runtime chat must talk to the selected coding CLI directly. Authentication
// status commands are setup/readiness diagnostics: running one before every
// message can hang independently of the retained tmux and block a valid turn.
// The Cursor adapter detects the CLI's actual login screen and returns a typed
// authentication-required error when startup genuinely needs login.
func TestHandleQueryDoesNotProbeCodingCLIAuthStatus(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	if err != nil {
		t.Fatalf("parse server.go: %v", err)
	}

	forbidden := map[string]bool{
		"cursorCLIStatusJSON":          true,
		"cursorCLILocalAuthState":      true,
		"providerAuthConfigured":       true,
		"cursorCLILocalAuthConfigured": true,
	}
	foundHandler := false
	ast.Inspect(file, func(node ast.Node) bool {
		decl, ok := node.(*ast.FuncDecl)
		if !ok || decl.Name.Name != "handleQuery" {
			return true
		}
		foundHandler = true
		ast.Inspect(decl.Body, func(child ast.Node) bool {
			call, ok := child.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if ok && forbidden[ident.Name] {
				t.Errorf("handleQuery calls %s; runtime messages must not run coding-CLI auth/status probes", ident.Name)
			}
			return true
		})
		return false
	})
	if !foundHandler {
		t.Fatal("handleQuery function not found in server.go")
	}
}
