# Development notes

## Building: upstream dependencies are pinned to shared feature branches

`agent_go` depends on two **private** modules that currently live on shared
feature branches (not `main`), because active work (the coding-agent transport
lanes, native `--resume`, bridge-routing options, the Agy removal) is still on
those branches:

| Module | Repo | Pinned branch |
| --- | --- | --- |
| `github.com/manishiitg/mcpagent` | `manishiitg/mcpagent` | `fix/family-server-custom-tools-bridge-allowlist` |
| `github.com/manishiitg/multi-llm-provider-go` | `manishiitg/llm-provider-mcp` (note: repo name ≠ module path — GitHub redirects) | `feat/claude-transcript-streaming` |

They are pinned as normal `require` pseudo-versions in `go.mod` (no local
`replace => ../../…` directives), so the build is reproducible on any machine
without needing sibling checkouts on a particular branch.

### First-time setup on a new machine

```bash
# 1. These modules are private — tell Go not to use the public proxy/checksum DB.
go env -w GOPRIVATE='github.com/manishiitg/*,github.com/city-mall/*'

# 2. Make sure git can reach the private repos over SSH
#    (git@github.com:manishiitg/…). `go build` fetches the pinned commits.
go build ./...
```

If `go build` fails with `undefined: mcpagent.WithBridgeRoutingInstructions`
(or any other missing upstream symbol), your pinned versions are stale or
`GOPRIVATE` is unset — see below.

### Picking up new upstream commits (the pin does NOT auto-follow the branch)

A branch pin resolves to a single commit; new commits pushed to those branches
are **not** picked up until you re-pin:

```bash
go get github.com/manishiitg/mcpagent@fix/family-server-custom-tools-bridge-allowlist
go get github.com/manishiitg/multi-llm-provider-go@feat/claude-transcript-streaming
go mod tidy
go build ./...
```

When those upstream branches eventually merge to their own `main`, switch the
pins to a tagged release (or `@main`) and delete this section.

## Pre-commit hook

The shared pre-commit hook currently has a `cd agent_go` path bug that fails
when committing from *inside* `agent_go` (see the `fix/pre-commit-hook-worktree-root`
branch). Until that lands, if a commit is blocked only by the hook's build step
— and `go build ./...` + `go vet ./...` pass locally and `gitleaks` is clean —
`git commit --no-verify` is acceptable.
