# Bug Report: Parallel Tool Execution Stuck Due to Lock Contention

## Status: Fixed ✅

## Symptoms
- Agents executing parallel tool calls (specifically file operations) would hang indefinitely or for long periods.
- Logs showed tools starting execution but timing out or getting "stuck" when multiple file operations were requested simultaneously.
- The issue was particularly noticeable when tools like `create_document`, `update_document`, or `move_document` were called in parallel with `read_document` or other read operations.

## Root Cause
### 1. Excessive Lock Duration (The Bottleneck)
The application uses an in-memory `LockManager` to ensure file consistency. The handlers for file modifications (`CreateDocument`, `UpdateDocument`, etc.) acquired an exclusive lock on the file/folder and held it for the **entire duration** of the request.

Crucially, these handlers perform a `SyncWithGitHub` operation (commit -> pull -> push) *before* returning. This network operation can take several seconds (2-5s+). Because the file lock was deferred (`defer lockManager.ReleaseLock(lock)`), it was held during this slow network call.

### 2. Lock Starvation
Parallel tool calls (like reading a file immediately after writing it, or two tools writing to different files but triggering repository-wide locks if implemented improperly) would block waiting for these locks to be released. Since the locks were held during network I/O, the queue of waiting tools would grow, eventually leading to timeouts or deadlocks.

### 3. Missing Read Safety
While write operations were locked, read operations (`GetDocument`) were initially lock-free (relying on OS atomicity). However, this introduced a race condition where a reader could read a file that was just truncated (0 bytes) by a writer before the content was flushed, or read a file in an inconsistent state during a multi-step move operation.

## The Fix
### 1. Optimized Critical Section (Reduce Scope)
Modified all file modification handlers (`CreateDocument`, `UpdateDocument`, `MoveDocument`, `DeleteDocument`, etc.) in `workspace/handlers/documents.go`.
- **Explicit Release:** The exclusive file lock is now explicitly released **immediately after** the filesystem operation completes.
- **Background Sync:** The `SyncWithGitHub` operation is performed **after** the lock is released. This allows other tools to access the file immediately while the Git sync happens safely in the background of the request logic (but still blocking the HTTP response, just not the file resource).

### 2. Safer Locking Primitives
Updated `workspace/utils/lock.go`:
- Added `AcquireReadLock`: A non-blocking (or short timeout) check to ensure a file isn't currently locked for writing.
- Improved `IsLocked` to be safer and removed potentially racy lock logic.

### 3. Read-Side Protection
Updated `GetDocument` to use `AcquireReadLock` with a short timeout. This ensures readers wait for active writers to finish (preventing 0-byte reads) but don't get blocked by the subsequent long-running Git sync.

## Verification
- Verified by inspecting `workspace/handlers/documents.go` to ensure `lockManager.ReleaseLock(lock)` is called before `utils.SyncWithGitHub`.
- Confirmed that `AcquireReadLock` is used in `GetDocument`.
- Validated that parallel execution of file writing followed by reading no longer hangs.
