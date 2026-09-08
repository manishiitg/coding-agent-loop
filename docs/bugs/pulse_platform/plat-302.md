[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-302 — Server deployments expose unusable CDP and can hit cross-user lock permissions

| Coordination | Value |
|---|---|
| Assigned agent | Codex |
| Ticket state | Implemented locally — deployment pending |
| Last synchronized | 2026-09-09 |
| Priority | P3 — CDP mode is not a standing, supported mode on a headless server deployment; low likelihood, confusing when it hits |

## Problem

`agent-browser`'s CDP mode connects to an already-running, shared Chrome
instance on a fixed port (default 9222) instead of launching its own
disposable browser per call. `pkg/browser/executor.go`'s CDP path coordinates
concurrent callers against that shared browser with two lock layers
(`pkg/browser/cdp_tabs.go`):

1. An in-process `sync.Mutex` (`sharedCDPLocks`), scoped per Go process.
2. A cross-process file lock (`flock`) at:

   ```go
   func sharedCDPFileLockPath(port int) string {
       return filepath.Join(os.TempDir(), fmt.Sprintf("mcp-agent-builder-cdp-%d.lock", port))
   }
   ```

   opened `os.O_CREATE|os.O_RDWR, 0600` (`acquireSharedCDPLock`,
   `cdp_tabs.go:76-120`).

That path is keyed **only by port number**, in the system-wide temp
directory, with no product or user identity in the name. Dominion, confida,
and RTS (Video Studio) all run on the same physical Hetzner host as separate
Unix accounts, each with its own `agent-browser` invocations, and all
default to the same CDP port. Whichever product's process creates the lock
file first owns the persistent inode at mode `0600` (owner-only); every other
product's process that later tries to open the same path for read/write gets
`EACCES`. The kernel releases the `flock` when the owning process exits, but
the owner-only inode remains, so another Unix user still cannot open it.

Live symptom reported from an agent session on the shared box:

```
Direct agent_browser: still permission denied on the shared CDP lock file
(/tmp/mcp-agent-builder-cdp-9222.lock), even though status reports the CDP
port itself as reachable.
```

`status` reporting the port "reachable" is a separate HTTP check of Chrome's
`/json/version` metadata and does not acquire this lock. It can therefore
report a valid Chrome endpoint even while this process cannot open the
cross-user lock file.

## Reframing during investigation

The obvious framing — "scope the lock path per-user/per-product so
Dominion/confida/RTS stop colliding" — turned out to be solving for the
wrong scenario. Checked live on the shared box: **no Chrome process is
normally listening with `--remote-debugging-port` at all.** There is no
persistent, standing shared-browser service for CDP mode to connect to on a
headless server — confirmed by the box owner ("on server there is no cdp as
there is no real browser").

The one CDP-listening Chrome process observed earlier in this session
(`confida`, port 9222, `--headless=new --remote-debugging-port=9222`) was a
transient artifact of some *headless* `agent-browser` launch — headless
Chrome apparently opens its own CDP port internally as an implementation
detail of that launch, not a durable shared daemon other sessions are meant
to attach to. It's gone by the time of a later `pgrep` check.

So the actionable bug is not how a valid shared CDP browser should coordinate:
these server products have no operator-owned visible Chrome for CDP in the
first place, but their configuration, tools, guidance, and UI still advertised
the mode. That let an agent select an unsupported path and encounter the lock
implementation as a secondary failure.

## Decided direction

Per the box owner: **agents should not use CDP mode on a server deployment
at all** — there is no real, standing browser for it to attach to there,
so the durable fix is steering agent guidance/tooling away from CDP mode on
server-side deployments (headless launch, matching `preview_report`'s own
approach, is the correct default), not scoping the lock path. The deployment
must declare this limitation once and every consumer must obey it.
Desktop/local installations retain CDP support.

## Implemented fix

`AGENT_BROWSER_CDP_ENABLED=false` is now the deployment capability for remote
servers (default remains enabled for backwards-compatible desktop/local use).

1. `agent_browser status` returns `cdp_supported=false`, a deployment-policy
   reason, and effective `headless` for workflow `auto`; it makes no CDP probe.
2. Explicit `--cdp`, `browser_mode=cdp`, and `cdp_ports` requests are rejected
   at execution, query, workflow-tool, and manifest-update boundaries.
3. The Builder tool schema excludes `cdp` dynamically and explains that
   `auto` is headless-only on this server.
4. Browser guidance/skills tell the agent to obey `status.cdp_supported` and
   never probe/install/configure CDP when it is false.
5. The runtime frontend config exposes `cdpEnabled: false`; both chat Browser
   Access and workflow Browser settings keep the CDP card visible but disabled
   with a clear “Disabled on server” label.
6. RTS, confida, Dominion, legacy dedicated-VM, and Kubernetes deployment paths
   set or validate the server capability.

The cross-user lock implementation is deliberately unchanged. If a future
server product introduces a real shared-browser service, it needs a separate
design for browser ownership and cross-product locking rather than silently
re-enabling this desktop feature.

## Verification

- [x] Unit coverage proves disabled `auto` resolves to headless without any CDP
      probe and explicit `--cdp` is rejected.
- [x] Server request/manifest policy coverage proves CDP settings are rejected
      and stale candidate ports are cleared.
- [x] Guidance coverage asserts the deployment-policy instructions remain in
      the Browser skill.
- [x] Frontend TypeScript build passes with both UI surfaces capability-aware.
- [ ] Deploy and smoke-test the public RTS server.

## Deployment

Pending. Deployment files are prepared; no server was changed by this local
implementation pass.
