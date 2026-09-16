# Bug: Chrome's own ProcessSingleton lock survives the dead-session auto-recovery path

## Status

Fixed in code (2026-09-16), not yet deployed. Found and root-caused live on
SparkQuill's Hetzner production deployment (`sparkquill.agentworkshq.com`,
persistent shared Chrome profile). Change is in `agent_go/pkg/browser/executor.go`
and `workspace/browserconfig/launch.go`; not yet built, committed, or shipped to
the server.

## Symptom

A family's real login/browsing session on SparkQuill would work, then Chrome
would crash once, and from that point on **every subsequent `agent_browser`
call for that session failed forever**, even though only one Chrome process was
ever running:

```
ERROR: tool execution failed... Chrome exited early (exit code: 21)
...ERROR:chrome/browser/process_singleton_posix.cc:1043] Failed to create socket directory.
...ERROR:chrome/app/chrome_main_delegate.cc:527] Failed to create a ProcessSingleton
for your profile directory. Aborting now to avoid profile corruption.
```

This looked, on the surface, like a multi-daemon collision — two Chrome
processes fighting over one profile — and the first several rounds of
investigation treated it as such (stale `agent-browser` daemons left behind by
manual testing and repeated `systemctl restart` during deploys, cleaned up with
`pkill` + removing `Singleton*` files by hand). That diagnosis was real and did
fix things for a while, but the problem came back with a **confirmed single
daemon** in play, which ruled out "leftover second process" as the ongoing
cause.

## Root Cause

Chrome enforces one live instance per profile directory using its own
`ProcessSingleton` mechanism: `SingletonLock`, `SingletonSocket`, and
`SingletonCookie` files inside the profile directory. If Chrome exits
uncleanly (crash, OOM-kill, `SIGKILL` from a restart), it does not always
remove these files. Their mere *presence* on the next launch attempt is enough
to make Chrome refuse to start — it cannot tell from the files alone whether
the old process is really dead, so it aborts rather than risk profile
corruption.

`agent_go/pkg/browser/executor.go` already had a recovery path for exactly
this class of failure: on a "dead session" error it kills the daemon, calls
`removeSessionFiles(session)`, sleeps, and lets the next call retry against a
fresh runtime. But `removeSessionFiles` only clears **agent-browser's own**
bookkeeping (`.pid`/`.sock`/etc.) — never Chrome's `Singleton*` files. So the
retry launched Chrome against a profile that still had a lock from the crash
that triggered the recovery in the first place, and immediately hit
`ProcessSingleton` again. Every automatic retry after any crash was guaranteed
to fail the same way, forever, with no code path that ever cleared it. Manual
intervention (deleting the lock files by hand) was the only thing that ever
"fixed" it — which is why the bug kept reappearing after each such crash even
once the multi-daemon issue was genuinely cleaned up.

There was a second, independent gap: the recovery path itself only triggers
when `isDeadSession(err)` returns true, which matched two specific
agent-browser/CDP error strings (`"CDP response channel closed"`,
`"No such file or directory"`). The `ProcessSingleton` failure text is a
*different* string and did not match either — so on the very first occurrence
after a crash, the error went straight back to the caller with no recovery
attempt at all. The two gaps compounded: even a caller that retried by hand
would hit the same un-recovered lock every time.

## Fix

`workspace/browserconfig/launch.go`: extracted the per-session profile-path
computation (previously inlined only in `HeadlessArgsForSession`) into an
exported `ProfilePathForSession(session string) string`, and re-exported it
from `agent_go/pkg/browser/launch.go` so the executor can resolve the same
on-disk profile directory the launcher used.

`agent_go/pkg/browser/executor.go`:

1. `isDeadSession` now also matches `"ProcessSingleton"`, so a crash-induced
   lock failure triggers the same auto-recovery path as a dead CDP connection
   instead of failing straight through.
2. The recovery path calls a new `removeStaleChromeSingletonLock(session)`
   right after killing the daemon and removing agent-browser's own session
   files. It resolves the session's profile directory via
   `ProfilePathForSession` and removes `SingletonLock`, `SingletonSocket`, and
   `SingletonCookie` if present, logging when it actually removed something.

This only ever removes lock files for a session whose runtime this same call
just killed moments earlier — it does not touch another session's profile,
and it is a no-op in ephemeral/session-isolated mode (`ProfilePathForSession`
returns `""` when there's no shared profile configured).

## Verification status

Build is clean (`go build ./pkg/browser/... ./cmd/server/...` in `agent_go`,
`go build ./...` in `workspace`). **Not yet**: unit test, commit, push,
rebuild server binaries, deploy, or confirmed against a real crash-and-recover
cycle. Per an explicit commitment made during this investigation, verification
must not be done against the family's real live browser session identity
(`user-37a8eec1ce19687d--browser`) on the production server — needs a
synthetic session or a controlled kill-and-relaunch test instead.

## Related

Earlier rounds of the same investigation (documented only in chat, not yet
written up separately) found and fixed:

- `workspace/security/landlock_runner_linux.go`: the Landlock write-path grant
  for the shared browser profile covered the bare profile dir but not its
  `-users`/`-workflows` siblings, where per-session persistent profiles
  actually live.
- Multiple stale/orphaned `agent-browser` daemons from manual testing and
  `systemctl restart` during deploys were themselves colliding on the same
  profile — a real but separate contributing cause, fixed operationally
  (`pkill`) rather than in code, and the practice of testing against the real
  session identity was stopped once identified as self-inflicted.
