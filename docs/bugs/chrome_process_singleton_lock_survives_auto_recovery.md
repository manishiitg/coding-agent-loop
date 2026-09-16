# Bug: Chrome's own ProcessSingleton lock survives the dead-session auto-recovery path

## Status

Deployed 2026-09-16 to SparkQuill's Hetzner production deployment
(`sparkquill.agentworkshq.com`, persistent shared Chrome profile), where this
was found and root-caused live. The original fix (clearing the lock files,
matching `ProcessSingleton` in `isDeadSession`) was not sufficient on its
own — see **Follow-up: the single retry can still lose the race** below,
fixed and deployed the same day.

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

Deployed to production via `deploy/rootless-linux/deploy.sh sparkquill` and
confirmed present in the running binary (`strings` matched the new log line).

## Follow-up: the single retry can still lose the race

The morning of the same day this shipped, a parent reported the family's
*real* login session still hit the identical `ProcessSingleton` error on
every attempt. Reading `agent.log` showed the fix firing correctly —
`isDeadSession` matched, `removeStaleChromeSingletonLock` removed the lock
files, the 2-second pause elapsed — and the **retry itself** still failed
with the exact same error, all within about 4 seconds end to end.

Reproducing by hand (same profile path, same session name, via the
`agent-browser` CLI directly, minutes later) succeeded immediately on the
first attempt. That rules out a permanently broken profile — the profile and
the lock-clearing logic are both fine — and points at a timing race instead:
`killSessionRuntime` fires `SIGKILL` at the daemon and Chrome's process group
but never confirms they have actually exited before the fixed 2-second sleep
starts counting down. The existing sleep's own comment already names this
general class of problem ("the killed process hasn't fully released its
pages yet") — 2 seconds just isn't always enough, especially on this shared
box with several other products' browser automation and thousands of leftover
Chrome temp files under `/tmp` from unrelated accounts.

**Fix**: if the retry *itself* still fails with `ProcessSingleton` (not just
the original error), run the same kill+cleanup sequence again and retry once
more after a longer, 5-second pause, in `agent_go/pkg/browser/executor.go`.
Bounded at one extra attempt — this treats a same-error-twice retry as a
timing problem worth one more try, not as a reason to loop indefinitely.

## Follow-up 2: three other kill sites had the exact same gap

Same day, still live. A later failure on the family's real session showed a
different shape: the very *first* `open` attempt (not a retry) failed with
`ProcessSingleton`, and — tellingly — the recovery path's own lock-removal
step found nothing to remove (no "Removed stale Chrome singleton lock" log
line), meaning the profile wasn't stale from a crash this time.

Two investigative steps ruled out the leading alternative theories before
finding the real cause:

- **Resource contention on the shared host.** The box had three unrelated,
  long-running tenant processes each pinned near 100% CPU for days (one for
  94 days straight). All three were killed. The failure still reproduced
  immediately afterward — contention wasn't sufficient to explain it, though
  it may still be a contributing factor under heavier load.
- **The Landlock sandbox that `execute_shell_command` runs under** (the
  `agent_browser` tool doesn't launch Chrome directly — it shells out through
  `sparkquill-workspace`'s sandboxed `/api/execute`, a different code path
  than a plain interactive shell). A live A/B test — identical command,
  identical clean profile state, only the sandbox present or absent —
  reproduced the failure sandboxed and succeeded unsandboxed. That looked
  like a smoking gun, but it didn't survive a cleaner retest: with the
  profile and process state genuinely verified clean beforehand (no leftover
  daemon from earlier manual testing), and even with the exact `FolderGuard`
  the real session sends, the sandboxed path succeeded too. The earlier
  "sandboxed fails" result was contaminated by the investigator's own
  leftover test process still holding the profile lock — a repeat, mid
  investigation, of the exact class of bug being investigated.

The real cause: `killSessionRuntime` + `removeSessionFiles` (kill the
process, clean agent-browser's own bookkeeping) is called at **six** sites in
`pkg/browser`, and the original fix above only added the singleton-lock
cleanup to two of them (the crash-recovery path in `HandleAgentBrowser`).
Three other sites had the identical gap and were never touched:

- `session_tracker.go`'s idle-session reaper (fires every 2 minutes, closes
  sessions idle >15 minutes) — the family's session was reaped this way 27
  minutes before the failure above.
- `executor.go`'s per-agent auto-eviction (frees a session slot for a new
  `open` when a limit is hit).
- `cleanup.go`'s `KillAllTrackedSessions`, which runs on every server
  `SIGTERM` — **every deploy**. Any deploy that landed while a browser
  session was tracked could leave a stale lock behind.

**Fix**: added `killSessionRuntimeFully(session)` — kill, remove
agent-browser's files, remove Chrome's stale lock, all three, always
together — and replaced every `killSessionRuntime` + `removeSessionFiles`
pair across all six call sites with it, so a future call site can't
reintroduce this by only doing two of the three steps.

## Follow-up 3: a seventh call site, and the failure that's still open

Deployed follow-up 2, then the exact same shape recurred within the hour:
`open` succeeds, the very next command (`snapshot`) reports the session
dead seconds later. This one wasn't explained by any of the sites above —
no idle reap, no deploy, nothing in between. Auditing the same grep more
carefully (it should have been exhaustive the first time and wasn't) found
a **seventh** site: the `reset` command handler in `executor.go` — the
command whose own doc comment says "use this when open/close keep
failing," i.e. the manual recovery path — had the identical gap. Fixed the
same way.

Two other explanations were tried and ruled out for this specific
open-then-snapshot-fails shape:

- **Old vs. new headless Chrome.** `agent-browser` hardcodes
  `--headless=new`, which spins up an internal "top-chrome-webui" renderer
  that isn't present in the older, simpler headless implementation, and
  every crash dump collected all day is a renderer crash. Tried appending
  `--headless=old` to our own `--args` (Chrome normally takes the last
  value for a repeated switch) hoping to override it. It didn't take —
  the `top-chrome-webui` renderer still spawned, confirming Chrome doesn't
  apply last-wins semantics to this particular switch. Not pursued further;
  we don't control agent-browser's own flag construction to fix this
  properly.
- Resource contention and the Landlock sandbox were already ruled out in
  Follow-up 2.

**What's still genuinely unresolved**: why the very first non-open command
against a browser that just opened successfully seconds earlier can report
the session dead. Added `logPreKillDiagnostics` (logged as
`[BROWSER_DIAG]`), called both right before any recovery kill and as a
baseline right before every non-open command runs against an already-open
session. It records whether the daemon and Chrome's stored PIDs are still
alive, a broader `pgrep` scan (in case the daemon respawned Chrome under an
untracked PID), and the age of any Singleton lock files present. This is
diagnostic only — no behavior change — added specifically so the next
occurrence has hard evidence instead of another round of inference.

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
