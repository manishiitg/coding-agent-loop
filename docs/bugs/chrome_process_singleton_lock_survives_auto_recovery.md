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
