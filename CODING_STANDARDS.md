# Coding Standards

## 🎯 Overview
This document outlines coding standards for the MCP Agent project (Go backend + TypeScript/React frontend).

---

## 🔷 Go Code Standards

### 1. Package Organization
- **Internal packages** (`internal/`): Not importable outside module
- **Public packages** (`pkg/`): Reusable across modules
- **Package imports**: Standard library → External → Internal
- **Package naming**: Short, lowercase, no underscores

### 2. Naming Conventions
- **Types**: `PascalCase` (e.g., `TodoExecutionAgent`, `ValidationResponse`)
- **Functions**: `camelCase` for private, `PascalCase` for exported
- **Receivers**: 2-3 char abbreviations (e.g., `tva`, `tea`, `teo`)
- **Constants**: `PascalCase` with type prefix (e.g., `SimpleAgent`, `ExecutionAgentType`)
- **Interfaces**: Descriptive names ending in `-er` or `Interface`
- **JSON tags**: `snake_case` (e.g., `json:"is_success_criteria_met"`)

### 3. Struct Design
- **Composition over inheritance**: Embed structs to extend functionality
- **Exported types**: Must have documentation comments
- **JSON tags**: Include `omitempty` for optional fields
- **Template structs**: Create dedicated types for template variables
- **Custom unmarshalers**: Handle flexible JSON formats when needed

### 4. Error Handling
- Always return errors, never panic in production code
- Wrap errors with context: `fmt.Errorf("operation failed: %w", err)`
- Check all errors (use `_ = ...` explicitly if intentionally ignoring)
- Use `errors.Is()` and `errors.As()` for error type checking
- Document error conditions in function comments

### 5. Context Management
- Always pass `context.Context` as first parameter
- Use custom context key types to avoid collisions (`type contextKey string`)
- Propagate context through all layers
- Respect context cancellation
- Never store context in structs

### 6. Concurrency & Thread Safety
- Use `sync.Mutex` to protect shared state
- Document goroutine ownership and lifecycle
- Close channels properly (only by sender)
- Use `sync.WaitGroup` for goroutine coordination
- Prevent goroutine leaks with proper cleanup

### 7. Event System
- Use typed event constants from `EventType` enum
- Implement `GetEventType()` interface for all event data
- Include hierarchy fields: `ParentID`, `HierarchyLevel`, `SessionID`
- Emit events at operation boundaries (start/end)
- Use appropriate component tags: `orchestrator`, `agent`, `llm`, `tool`

### 8. Agent Development
- Extend `BaseOrchestratorAgent` or `BaseAgent` for consistency
- Use agent type constants from `AgentType` enum
- Implement `Execute()` or `ExecuteStructured()` methods
- Create custom input processors for prompt generation
- Implement retry logic with clear max attempts
- Auto-save progress to JSON files

### 9. LLM Integration & Structured Output
- Define JSON schemas inline or as constants
- Use generic type-safe methods for structured output
- Template variables use `CamelCase` in prompt templates
- Response structs must match LLM JSON output exactly
- Separate system prompts from user messages
- Use tool calling for structured output validation

### 10. Template & Prompt Engineering
- Use `text/template` package for complex prompts
- Create dedicated template variable structs
- Use conditional sections with `{{if}}` blocks
- Structure prompts with clear markdown formatting
- Include sections: Agent Identity, File Permissions, Task, Output Format
- Validate template execution errors

### 11. File & Path Handling
- Use `filepath.Join()` for cross-platform path construction
- Check file existence before operations
- Handle file I/O errors properly
- Document expected file locations and formats
- Use workspace-relative paths consistently

### 12. Testing
- Test files: `*_test.go` alongside implementation
- Table-driven tests for multiple scenarios
- Test naming: `TestFunctionName_Scenario`
- Mock interfaces for external dependencies
- Integration tests use separate build tags

### 13. Security
- **Never** commit secrets or credentials
- Use environment variables for sensitive data
- Placeholder values in `.env.example` files
- Run `gitleaks` scan before commits
- Use `//nolint:gosec` with justification for false positives
- Add comments for credential-like constants (e.g., `//nolint:gosec // G101: Event type constant`)

### 14. Logging
- Use `ExtendedLogger` interface consistently
- Log levels: `Error`, `Warn`, `Info`, `Debug`
- Include context in log messages
- Redact sensitive data from logs
- Use structured logging for machine readability

### 15. Code Quality
- Run `make lint` before commits (uses `golangci-lint`)
- Fix critical linter issues: `gosec`, `errorlint`, `misspell`
- All exported types must have documentation comments
- Keep functions focused and under 100 lines when possible
- Avoid magic numbers (use named constants)

---

## 🔷 TypeScript/React Standards

### 1. Type Safety
- Use strict TypeScript with no `any` types
- Define interfaces for all API responses
- Export types from `api-types.ts`
- Use discriminated unions for event types
- Avoid type assertions unless necessary

### 2. Component Organization
- **Hooks**: Custom hooks in `hooks/` directory
- **Stores**: Zustand stores in `stores/` directory
- **Services**: API clients in `services/` directory
- **Components**: Organized by feature/domain
- **Types**: Shared types in `types/` directory

### 3. React Best Practices
- Use functional components with hooks
- Clean up side effects in `useEffect` returns
- Implement proper error boundaries
- Memoize expensive computations with `useMemo`
- Use `useCallback` for event handlers passed as props
- Extract complex logic to custom hooks

### 4. State Management
- Global state: Use Zustand stores
- Local state: Use `useState` for simple cases
- Server state: Use proper loading/error states
- Derive state when possible instead of storing

### 5. API Integration
- Centralize API calls in service files
- Use Axios with proper error handling
- Define request/response types
- Handle loading and error states
- Implement proper timeout handling

### 6. Event Handling
- Clean up event listeners properly
- Use typed event handlers
- Prevent memory leaks with cleanup
- Handle errors in event handlers

---

## 🔷 Documentation Requirements

### 1. Code Comments
- All exported Go types/functions must have comments
- Start comments with the element name
- Explain *why*, not just *what*
- Document complex algorithms
- Add TODO comments with context

### 2. README Files
- Each major subsystem should have a README
- Include architecture diagrams for complex flows
- Provide usage examples
- Document configuration options
- Explain key concepts

### 3. API Documentation
- Document all API endpoints
- Include request/response examples
- Specify error codes and messages
- Document authentication requirements

---

## 🔷 Git Workflow

### 1. Branch Naming
- Feature: `feature/description`
- Bug fix: `bugfix/description`
- Hotfix: `hotfix/description`
- Frontend: `frontend/description`
- Backend: `backend/description`

### 2. Commit Messages
- Use conventional commits format
- Start with type: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`
- Keep first line under 72 characters
- Add detailed description in body if needed

### 3. Pull Requests
- Clear title describing the change
- Reference related issues
- Include testing steps
- Update documentation if needed
- Ensure CI passes before merging

---

## ✅ Code Review Checklist

### Go Code
- [ ] All exported types have documentation comments
- [ ] Error handling is comprehensive
- [ ] Context is properly propagated through all layers
- [ ] No goroutine leaks (proper cleanup)
- [ ] JSON marshaling/unmarshaling tested
- [ ] Events emitted at proper boundaries
- [ ] Tests included for new functionality
- [ ] Linter passes (`make lint`)
- [ ] No secrets committed (`gitleaks` scan passed)

### TypeScript/React Code
- [ ] No `any` types used
- [ ] Proper TypeScript interfaces defined
- [ ] useEffect cleanup implemented
- [ ] Error boundaries in place
- [ ] API types exported properly
- [ ] No console.log in production code
- [ ] Accessibility considerations addressed

### General
- [ ] README updated if adding features
- [ ] Database migrations if schema changed
- [ ] API types updated if endpoints changed
- [ ] Configuration documented
- [ ] Performance implications considered
- [ ] Security implications reviewed

---

## 🔧 Development Tools

### Go
- **Linter**: `golangci-lint` (config: `.golangci.yml`)
- **Testing**: `go test ./...`
- **Build**: `make build`
- **Format**: `gofmt` and `goimports`

### TypeScript/React
- **Linter**: ESLint (config: `eslint.config.js`)
- **Type checking**: `tsc --noEmit`
- **Format**: Prettier (if configured)
- **Build**: `npm run build`

### Security
- **Secret scanning**: `gitleaks` (config: `.gitleaks.toml`)
- **Dependency scanning**: `npm audit`, `go list -m all`
- **Pre-commit hooks**: Install with `./scripts/install-git-hooks.sh`

---

## 📚 Additional Resources

- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Effective Go](https://go.dev/doc/effective_go)
- [React Best Practices](https://react.dev/learn)
- [TypeScript Handbook](https://www.typescriptlang.org/docs/handbook/intro.html)
- [Conventional Commits](https://www.conventionalcommits.org/)

---

**Remember**: Write code for humans first, machines second. Prioritize clarity over cleverness.

