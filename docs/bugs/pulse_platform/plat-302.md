[← Pulse platform index](../pulse_platform_issue_register.md)

# PLAT-302 — Shared CDP lock file can strand agent-browser with a permanent permission error across products on the same box

| Coordination | Value |
|---|---|
| Assigned agent | Claude Code |
| Ticket state | Open — identified live, not yet fixed (explicitly deferred by request) |
| Last synchronized | 2026-09-08 |
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
file first owns it at mode `0600` (owner-only); every other product's
process that later tries to open the same path for read/write gets a
permanent `EACCES`, and the file never self-cleans (nothing removes it when
the owning process exits).

Live symptom reported from an agent session on the shared box:

```
Direct agent_browser: still permission denied on the shared CDP lock file
(/tmp/mcp-agent-builder-cdp-9222.lock), even though status reports the CDP
port itself as reachable.
```

`status` reporting the port "reachable" is a separate, unauthenticated TCP
check unrelated to the lock file — it can report true even while the lock
acquisition fails, which is what made this confusing to diagnose from
inside a workflow session.

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

So the real bug shape is: two products' independent headless launches can
transiently overlap on the same default port, one leaves behind a
permission-locked lock file when its process exits without cleanup, and a
later session's `status` check can then misreport CDP as "reachable" (because
something briefly answered on that port) even though nothing durable is
there — sending a workflow down the CDP path only to hang on the stale lock.

## Decided direction (not implemented here)

Per the box owner: **agents should not use CDP mode on a server deployment
at all** — there is no real, standing browser for it to attach to there,
so the durable fix is steering agent guidance/tooling away from CDP mode on
server-side deployments (headless launch, matching `preview_report`'s own
approach, is the correct default), not scoping the lock path. That guidance
change is tracked separately from this ticket; this ticket is the underlying
lock-file bug record.

## Proposed fix (not implemented)

Two independent angles, either or both:

1. **Stop the stale-lock false positive.** The lock file is never removed
   when its owning process exits; a crash or abrupt exit between
   `os.OpenFile` and `syscall.Flock`'s unlock leaves a `0600` file another
   process can never open. At minimum, `status`'s CDP-reachability check
   should not be satisfied by an unrelated transient listener on the port —
   it should reflect whether *this* process can actually acquire the lock,
   not just whether something answered a TCP dial a moment ago.
2. **Scope the lock path if CDP mode is ever meant to be a real standing
   mode on a shared box** (e.g. local dev machines shared by a team, or a
   future durable shared-browser service) — include a per-user/per-product
   discriminator (`os.Geteuid()` or similar) in
   `sharedCDPFileLockPath`. Deferred because, per the decided direction
   above, CDP mode is not meant to be in standing use on these server
   deployments in the first place — fixing the lock path alone would not
   address the deeper "agents shouldn't reach for CDP here" issue.

## Verification

- [ ] Not yet reproduced as a clean repro — observed live via a real agent
      session's error report plus a manual `pgrep` check confirming no
      standing CDP listener, not a constructed test case.
- [ ] No fix implemented. Explicitly deferred by request — this ticket is a
      record, not a queued fix.

## Deployment

N/A — tracking only. No code changed for this ticket.
