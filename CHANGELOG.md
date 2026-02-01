# Changelog

## [Unreleased] - 2026-01-31

### Fixed
- **Auto-Unlock Loop**: Fixed a critical bug where Orchestration and Todo Task sub-agents triggered false-positive plan modifications due to dynamic runtime instructions. Implemented "Clone & Modify" pattern to preserve original plan integrity.
- **Stale UI Locks**: Fixed issue where UI showed steps as "Locked" after a learning reset because auto-lock metadata was not being cleared.
- **Todo Task Instructions**: Fixed broken instruction passing to Todo Task sub-agents by embedding instructions directly in step descriptions.

## [v0.1.0] - 2026-01-25

### Features

#### Skills System
- Add skills support for workflow mode (Phase 1 & 2)
- Skills discovery, parsing, and validation
- GitHub skills integration
- Workspace API for skills
- Skills selection UI components

#### Workspace Tools
- Refactor workspace tools into categories (basic, advanced, git, browser)
- Add web/PDF tools for document processing
- Browser automation tools support

#### Workflow Orchestration
- Add tool search mode support
- Evaluation system with scoring agent and debugger manager
- Code execution debugging agent
- Template registry for workflow steps
- Batch progress header and execution logs popup
- Running workflows drawer and indicator
- Evaluation and costs popups
- Workflow state normalization utilities

#### LLM Configuration
- Multiple LLM provider sections (Anthropic, OpenAI, Bedrock, Vertex, OpenRouter)
- Fallbacks and library tabs for LLM config
- Model options configuration
- LLM configuration handlers

#### OAuth & Authentication
- OAuth routes and status badge
- OAuth test component
- OAuth API service

#### Database
- Supabase support as database backend
- PostgreSQL migrations
- Database migration tooling

#### Frontend Improvements
- MCP config popup
- Circular progress component
- Model selector component
- Workflow canvas improvements with legend
- Step edit panel enhancements
- Tool search tool call display components

### Refactoring
- Update mcpagent import paths to github.com/manishiitg/mcpagent
- Optimize workflow components (logging, event handling, agent configs)
- Remove deprecated fields and improve batch execution performance
- Token usage tracking enhancements
- Error handling improvements

### Documentation
- Add evaluation system documentation
- Add event system documentation
- Add execution configuration documentation
- Add historical records documentation
- Add learnings and validation architecture documentation
- Add LLM configuration and resilience documentation
- Add OAuth documentation
- Add running workflows documentation
- Add skills documentation
- Add workflow monitoring documentation
- Update workflow orchestrator documentation
- Cleanup obsolete documentation files

### Bug Fixes
- Various fixes for golint
- Guard fixes
- Workspace diff fallback improvements
- Shell execution security enhancements

### Dependencies
- Update to github.com/manishiitg/mcpagent v0.2.0-pre.1
- Update to github.com/manishiitg/multi-llm-provider-go v0.3.0-pre.1
