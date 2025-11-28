# Refactoring Plan: Extract Core Agent to Separate Module

## Overview

Extract core agent logic from `agent_go/` into a new standalone Go module `mcpagent/` at the project root.

**Two separate PRs:**
- **PR 1**: Module extraction (this plan)
- **PR 2**: Logging system improvement (separate, after PR 1 merges)

---

## Part 1: Module Extraction

### Goal
Create a decoupled `mcpagent/` module containing the core agent, MCP client, caching, events, observability, and LLM wrapper. The `agent_go/` will retain server, orchestrator, and workflow logic.

### New Module Structure

```
mcpagent/                          # NEW - Separate Go module
├── go.mod                         # module mcpagent
├── go.sum
├── agent/                         # Core agent (from pkg/mcpagent/)
│   ├── agent.go
│   ├── conversation.go
│   ├── llm_generation.go
│   ├── tool_filter.go
│   ├── smart_routing.go
│   ├── virtual_tools.go
│   ├── code_execution_tools.go
│   ├── connection.go
│   ├── error_handler.go
│   ├── large_output_virtual_tools.go
│   ├── structured_output.go
│   ├── streaming_tracer.go
│   ├── event_listeners.go
│   ├── tool_utils.go
│   ├── utils.go
│   ├── codeexec/                  # Subpackage
│   └── prompt/                    # Subpackage
├── mcpclient/                     # MCP protocol client (from pkg/mcpclient/)
│   ├── client_interface.go
│   ├── http_manager.go
│   ├── sse_manager.go
│   ├── logger_adapter.go
│   └── ...
├── mcpcache/                      # Caching layer (from pkg/mcpcache/)
├── events/                        # Event types (from pkg/events/)
├── observability/                 # Tracing (from internal/observability/)
│   ├── tracer.go
│   ├── factory.go
│   └── langfuse_tracer.go
└── llm/                           # LLM wrapper (from internal/llm/)
    └── providers.go

# Note: logger/ will be added in PR 2
```

### What Stays in agent_go/

```
agent_go/
├── cmd/
│   ├── server/                    # HTTP server, REST API
│   ├── testing/                   # Testing CLI
│   ├── mcp/                       # MCP CLI commands
│   ├── schema-gen/
│   └── timeout/
├── pkg/
│   ├── orchestrator/              # Workflow orchestration
│   ├── external/                  # External API wrappers
│   ├── agentwrapper/              # Agent config wrapper
│   ├── database/                  # Database integration
│   ├── logger/                    # Existing logger (until PR 2)
│   └── utils/                     # Utilities (if needed)
├── internal/
│   ├── events/                    # Event store (stays - used by server)
│   └── utils/                     # Keep minimal utilities
├── configs/
└── schemas/
```

### Files to Move

| From | To |
|------|-----|
| `agent_go/pkg/mcpagent/*.go` | `mcpagent/agent/` |
| `agent_go/pkg/mcpagent/codeexec/` | `mcpagent/agent/codeexec/` |
| `agent_go/pkg/mcpagent/prompt/` | `mcpagent/agent/prompt/` |
| `agent_go/pkg/mcpclient/*.go` | `mcpagent/mcpclient/` |
| `agent_go/pkg/mcpcache/*.go` | `mcpagent/mcpcache/` |
| `agent_go/pkg/events/*.go` | `mcpagent/events/` |
| `agent_go/internal/observability/*.go` | `mcpagent/observability/` |
| `agent_go/internal/llm/*.go` | `mcpagent/llm/` |

### New go.mod for mcpagent/

```go
module mcpagent

go 1.21

require (
    github.com/mark3labs/mcp-go v0.x.x
    github.com/sirupsen/logrus v1.9.x
    // other dependencies from agent_go/go.mod
)
```

### Import Path Changes

**Old → New:**
```
mcp-agent/agent_go/pkg/mcpagent      → mcpagent/agent
mcp-agent/agent_go/pkg/mcpclient     → mcpagent/mcpclient
mcp-agent/agent_go/pkg/mcpcache      → mcpagent/mcpcache
mcp-agent/agent_go/pkg/events        → mcpagent/events
mcp-agent/agent_go/internal/observability → mcpagent/observability
mcp-agent/agent_go/internal/llm      → mcpagent/llm
```

### Files Requiring Import Updates (44 files)

**In agent_go/pkg/orchestrator/ (~20 files):**
- `base_orchestrator.go`
- `context_aware_bridge.go`
- `agents/base_agent.go`
- `agents/base_orchestrator_agent.go`
- All workflow agent files in `agents/workflow/`

**In agent_go/pkg/external/ (2 files):**
- `agent.go`
- `build_prompt.go`

**In agent_go/pkg/agentwrapper/ (1 file):**
- `llm_agent.go`

**In agent_go/cmd/server/ (2 files):**
- `server.go`
- `tools.go`

**In agent_go/cmd/testing/ (~14 files):**
- `agent.go`, `comprehensive-simple.go`, `code-execution-test.go`, etc.

### agent_go/go.mod Update

Add replace directive during development:
```go
require mcpagent v0.0.0

replace mcpagent => ../mcpagent
```

---

## Part 2: Logging System Improvement (Future PR 2)

> **Note**: This will be implemented in a separate PR after the module extraction is complete.
> Logger will live in `mcpagent/logger/` - keeps the module self-contained.
> Backend remains logrus (with cleaner interface).

### Current Problems

1. **3 incompatible interfaces**: `ExtendedLogger`, `interfaces.Logger`, `util.Logger`
2. **Adapter chain complexity**: Multiple adapters losing functionality
3. **Logrus dependency leakage**: Returns `*logrus.Entry` directly
4. **Global test logger**: Not thread-safe
5. **Inconsistent creation**: Each package creates its own logger
6. **Silent fallbacks**: `minimalLogger` silently fails

### New Unified Logger Design

Create a single, clean logging interface in `mcpagent/logger/`:

```go
// mcpagent/logger/interfaces.go

// Logger is the primary logging interface
type Logger interface {
    // Basic logging
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, err error, fields ...Field)
    Fatal(msg string, err error, fields ...Field)

    // Create child logger with preset fields
    With(fields ...Field) Logger

    // Resource cleanup
    Close() error
}

// Field represents a structured log field
type Field struct {
    Key   string
    Value interface{}
}

// Helper constructors
func String(key, value string) Field
func Int(key string, value int) Field
func Error(err error) Field
func Any(key string, value interface{}) Field
```

```go
// mcpagent/logger/logger.go

type Config struct {
    Level      string // debug, info, warn, error
    Format     string // text, json
    Output     string // stdout, stderr, file path
    EnableFile bool
    FilePath   string
}

func New(cfg Config) (Logger, error)
func NewDefault() Logger
func NewNoop() Logger  // For testing - does nothing but doesn't fail
```

### Benefits of New Design

1. **Single interface** - No more adapters
2. **No logrus leakage** - Implementation detail hidden
3. **Structured logging** - First-class support via `Field`
4. **Thread-safe** - No global state
5. **Testable** - `NewNoop()` for tests
6. **Swappable** - Can change to zap/zerolog later

### Migration Strategy

1. Create new `mcpagent/logger/` package
2. Implement with logrus initially (same backend, better interface)
3. Update `mcpagent/agent/` to use new logger
4. Create adapter for legacy code: `func ToExtendedLogger(l Logger) ExtendedLogger`
5. Gradually migrate `agent_go/` consumers

---

## Implementation Order (PR 1: Module Extraction)

### Phase 1: Setup New Module
1. Create `mcpagent/` directory at project root
2. Create `mcpagent/go.mod` with dependencies

### Phase 2: Move Core Packages
1. Copy `pkg/mcpagent/` → `mcpagent/agent/`
2. Copy `pkg/mcpclient/` → `mcpagent/mcpclient/`
3. Copy `pkg/mcpcache/` → `mcpagent/mcpcache/`
4. Copy `pkg/events/` → `mcpagent/events/`
5. Copy `internal/observability/` → `mcpagent/observability/`
6. Copy `internal/llm/` → `mcpagent/llm/`

### Phase 3: Update Internal Imports
1. Update all import paths within `mcpagent/` to use new module path
2. Replace old logger usage with new `mcpagent/logger`
3. Run `go mod tidy` in `mcpagent/`
4. Verify: `cd mcpagent && go build ./...`

### Phase 4: Update agent_go/
1. Add `replace mcpagent => ../mcpagent` to `agent_go/go.mod`
2. Update all 44 consumer files with new import paths
3. Run `go mod tidy` in `agent_go/`
4. Verify: `cd agent_go && go build ./...`

### Phase 5: Cleanup
1. Remove moved files from `agent_go/`
2. Remove unused dependencies from `agent_go/go.mod`
3. Run full test suite
4. Update any documentation

---

## Implementation Order (PR 2: Logger Improvement - Future)

### Phase 6: Logger Migration
1. Implement new logger in `mcpagent/logger/`
2. Update `mcpagent/agent/` to use new logger
3. Create legacy adapter for `agent_go/` consumers
4. Gradually migrate remaining code

---

## Verification Commands

```bash
# Build new module
cd mcpagent && go build ./...

# Build agent_go with new dependency
cd agent_go && go build ./...

# Run tests
cd mcpagent && go test ./...
cd agent_go && go test ./...

# Check for import cycles
go mod graph | grep cycle
```

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Circular dependencies | Move all dependent packages together |
| Breaking existing imports | Use `replace` directive during transition |
| Missing dependencies | Run `go mod tidy` after each phase |
| Test failures | Run tests after each phase |
| Build failures | Incremental approach - verify at each step |

---

## Critical Files to Read Before Implementation

1. `agent_go/go.mod` - Current dependencies
2. `agent_go/pkg/mcpagent/agent.go` - Main agent struct
3. `agent_go/internal/observability/langfuse_tracer.go` - Tracer implementation
4. `agent_go/pkg/logger/factory.go` - Current logger factory
5. `agent_go/internal/utils/extended_logger.go` - Current logger interface
