# Live browser in workflows

The workflow Browser automation tab shows the managed headless browser for that
workflow. It discovers sessions automatically when `agent_browser open` runs.
Select a browser session to watch its active tab. Browser settings are behind the gear button; the viewer fills the panel. Closed, completed, or reaped sessions disappear from the list.

Watch mode cannot send input or change the browser's active tab. Take control
requires workflow write access and exclusive access to that browser session.
If an agent action or another controller is using the browser, the request
returns a busy message. Once control is granted, subsequent managed browser
commands wait; unrelated workflow work can continue. Return control or Escape
releases them. Closing the viewer, switching sessions, or losing the connection
also releases control (45-second heartbeat timeout for silent disconnects).

## Sessions and tabs

A workflow can have multiple managed browser sessions, and each session can have
multiple tabs. The session selector chooses which browser to watch. The tab strip
shows that session's tabs, with the active tab highlighted. The viewer displays
one active tab at a time; it does not display all tabs simultaneously.

Normal browser commands update the existing session's live view automatically.
There is no separate viewer initialization command for each action. Session
discovery refreshes every five seconds while the panel is mounted. Live frames
arrive over WebSocket, with the proxy requesting a maximum of 10 frames per second.

Watching follows the agent's active tab. Switching tabs changes the actual
browser's active tab, so tab selection is enabled only after taking control.
Users with control can click, type, scroll, and select another tab. Escape returns
control to the agent. Manual control holds browser commands for that session;
it does not pause the entire workflow or unrelated sessions.

## Architecture

```text
Workflow Browser tab
    │ authenticated WebSocket through the app's existing HTTPS endpoint
    ▼
Agent API
    │ checks session owner and workflow; enforces watch/control mode
    │ connects using WORKSPACE_API_URL and WORKSPACE_API_TOKEN
    ▼
Workspace service (same host/container environment as agent-browser)
    │ resolves the session's .stream file to a loopback port
    ▼
agent-browser stream → managed headless Chrome
```

| Endpoint | Purpose |
| --- | --- |
| `GET /api/browser/live/sessions?workspace_path=Workflow/<folder>` | List the signed-in user's tracked browser sessions for the selected workflow. |
| `GET /api/browser/live/{session}/stream?workspace_path=Workflow/<folder>` | Authenticated viewer WebSocket on the agent API. |
| `GET /api/browser/live/:session/stream` | Internal workspace-service proxy to the local session stream. |

The frontend sends a heartbeat every 10 seconds. The server releases manual
control after a disconnect or 45 seconds without an incoming message. Only one
viewer connection can hold control of a session at a time. Other connections
can continue watching.

## Server deployment

This uses the shared agent API and workspace service, so the same implementation
works in native/rootless, Docker, and Kubernetes deployments. Deploy both backend
binaries and the frontend together. Install a streaming-capable agent-browser in
the workspace service's environment; tested with 0.37.0 and headless Chrome.
Existing native installations may need an agent-browser upgrade; presence-only
install checks do not upgrade an older binary. New images install the current
agent-browser package as before.

No dashboard daemon, public browser port, configured CDP endpoint, or local
Chrome is required. `AGENT_BROWSER_CDP_ENABLED=false` remains supported. The
headless browser uses its own internal browser protocol; that is independent of
the app's shared-CDP mode.

The browser connects to `/api/browser/live/{session}/stream` on the normal app
API with the existing login. The agent API checks the workflow and session owner,
then connects to `/api/browser/live/:session/stream` on `WORKSPACE_API_URL`,
carrying `WORKSPACE_API_TOKEN`. The workspace service resolves the session's local
`.stream` metadata and proxies to loopback. This keeps working when the agent API
and workspace service are in separate containers. Reverse proxies must forward
WebSocket upgrades on `/api/` (as for the existing live terminals); ingress must
allow an idle timeout longer than 45 seconds. No session port is published.

The generic workspace proxy rejects this internal stream route. The live viewer
only forwards viewport, tab, URL and connection status messages, and accepts
input only while its connection holds control. Shared desktop/CDP sessions are
not listed; they can contain tabs belonging to other workflows. Discovery is
limited to the signed-in user's active workflow sessions.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| No browser sessions appear | Run a browser-enabled workflow that opens a managed headless session. Confirm that the selected workflow and signed-in user own the active session. Completed or cleaned-up sessions are not retained for replay. |
| Session appears but live view cannot connect | Check the agent-browser version, its session `.stream` metadata, and whether streaming is enabled in the workspace service's runtime environment. |
| WebSocket connection fails | Check the existing HTTPS proxy's upgrade forwarding, `WORKSPACE_API_URL`, and matching workspace service tokens. Use Reconnect after correcting the issue. |
| Take control reports busy | Let the current browser action finish, or have the existing controller return control, then retry. |
| Cannot switch tabs while watching | Take control first; switching tabs affects the browser the agent is using. |
| Take control is unavailable or denied | Confirm workflow write access. Session visibility alone does not grant control. |

## Implementation files

- [WorkflowLiveBrowser.tsx](../frontend/src/components/workflow/WorkflowLiveBrowser.tsx): session discovery, viewport, tab strip, input, and connection lifecycle.
- [WorkflowCapabilitiesPanel.tsx](../frontend/src/components/workflow/WorkflowCapabilitiesPanel.tsx): embeds the viewer above browser settings.
- [Agent API browser_live.go](../agent_go/cmd/server/browser_live.go): workflow-scoped discovery, authenticated stream relay, and manual control.
- [live_control.go](../agent_go/pkg/browser/live_control.go): exclusive control gate shared with managed browser commands.
- [executor.go](../agent_go/pkg/browser/executor.go): waits on the control gate before running headless browser commands.
- [Workspace browser_live.go](../workspace/handlers/browser_live.go): resolves local stream metadata and proxies within the workspace environment.
- [workspace_proxy.go](../agent_go/cmd/server/workspace_proxy.go): prevents bypassing viewer access checks through the generic workspace proxy.

## Verification

- `cd workspace && go test ./handlers -run TestBrowserLive`
- `cd agent_go && go test ./pkg/browser -run TestBrowserControl`
- `cd agent_go && go test ./cmd/server -run TestLiveBrowser`
- `cd agent_go && RUN_LIVE_BROWSER_E2E=1 go test ./cmd/server -run TestLiveBrowserRealHeadless`

The opt-in integration test launches and closes its own headless browser and
checks live frames, tabs, mouse focus and typing through the workspace proxy.

Frontend validation: `cd frontend && npm run build`.

The implementation was validated locally with agent-browser 0.37.0: real headless
frames, tab discovery, mouse focus, typing, workflow isolation, watch-mode input
blocking, exclusive control, and disconnect recovery. Backend race checks and
desktop/mobile UI checks also passed. Container and Kubernetes topology support
comes from routing through the workspace service; it has not been verified by a
live rollout to each deployment.

## Deployment scope and rollout order

This feature applies to all three server-based deployments: RTS (Video Studio),
Dominion, and Confida. All use the shared agent API, workspace service, and
workflow Browser tab. Keep shared-CDP mode disabled on these servers.

| Deployment | Deployment entry point | Rollout order |
| --- | --- | --- |
| RTS / Video Studio | `deploy/aws-ec2/deploy-rootless.sh` | First: stage and verify the live-browser integration here. |
| Dominion | `deploy/dedicated-vm/deploy-dominion.sh` | Follow after RTS validation. |
| Confida | `deploy/confida/deploy-rootless-confida.sh` | Follow after RTS validation and restoration of server access. |

Each rollout must include the frontend, agent API, and workspace service, and
verify a real managed browser session while shared CDP remains disabled. Keep
browser session metadata in the same runtime environment as that deployment's
workspace service; do not point one deployment at another's stream ports.

## Rollout status

RTS was deployed on 2026-09-09 at `https://video.realtrainingsys.com`, using
release `live-browser-refresh-20260909` (focused source commit `0b9593dd0`). The release
includes the frontend, agent API, workspace service, and agent-browser 0.37.0.
Shared CDP is disabled. Agent, workspace, and gateway services passed health
checks after activation, and the public live-session endpoint rejects
unauthenticated requests.

Before activation, isolated real-browser smoke tests on the RTS server passed
live frames, two tabs, mouse focus, and typing through the staged workspace
proxy. Both Linux systemd runtime metadata and the restricted workflow
`HOME=/tmp` environment were checked. A signed-in production workflow UI run also passed: the `rts-latency`
workshop called `agent_browser status`, opened Google, and displayed the Google
page in the embedded live viewer with status **Watching**.
The previous release is retained for rollback.

Dominion and Confida remain pending. Confida/Hetzner access was blocked because
the configured SSH key was unavailable locally and other attempted access was
rejected. Roll out the same shared implementation and verify a real workflow
in each deployment after access is available.

### Persistent chat browser settings

The RTS UI check exposed a missing-tool bug when a chat started with **No
browser** and was later changed to **Automatic**. Persistent CLI turns reused
the original tool registration. The follow-up release keeps the workflow
browser tool registered and reads the current manifest on each invocation.
Disabled, missing, or unreadable configuration cannot launch a browser. The
regression test covers enabling and disabling the same tool instance without
creating another chat. Shared fix: `961d22b5a`.

## Builder view switching

Interactive Builder guidance requests `open_workspace_view(view="browser")`
when beginning browser navigation for the user. The tool opens the full browser
panel; the stream updates automatically, without repeated refresh requests.
Scheduled and unattended runs do not manipulate the foreground UI. The Builder
should respect subsequent user view changes.

When a builder workspace-view action changes the visible panel, a small toast
identifies it, for example “Builder opened Browser”. Acknowledged UI actions
notify only after an applied result. Reopening the same visible view or
refreshing it does not create another switch notification.

## Manual recording and viewer controls

Use **Start recording**, then **Stop recording** to export a capture under
`<workflow>/browser-recordings/<timestamp-id>/`. The bundle contains
`video.webm`, `network.har`, `console.json`, `errors.json`, `manifest.json`,
and `capture.zip`. HAR response bodies are omitted; URLs, timings and headers
remain. Console output is the browser runtime buffer for the capture interval.
Video records the page active when capture starts. Stop recording before
ending or cleaning up the browser session; abrupt browser termination may
leave partial output. The recording runs on the server even if the viewer
is disconnected. Start/stop requires workflow write access.

Clicking an inactive tab requests exclusive control and then switches tabs.
If the agent is busy, the viewer explains why control could not be acquired.
**Fill width** uses the full panel width with vertical scrolling; **Fit page**
keeps the entire viewport visible with its aspect ratio preserved.

Browser sessions are isolated by chat owner/session and browser session name.
Cookies survive while that browser context is running. Persistent profiles
across chat sessions, cleanup and server restarts are not enabled by this
viewer. A future remember-login option must isolate profiles per user and
workflow; it cannot guarantee that a site's login never expires.

### Recording rollout status (2026-09-09)

Recording, Fill width/Fit page, click-to-take-control tab switching, and the
launch-option consistency fix are pushed to shared `main` at `504c35a5e`.
The focused RTS release `2e949bf9c`, with toolbar/guidance overlay
`34a9b1d17`, was activated at 07:23 UTC (12:53 IST) on 2026-09-09 at
`releases/browser-recording-20260909`, after the scheduled security run
finished and the agent reported idle with zero active sessions/requests.
An initial 07:17 UTC activation was rolled back after the UI recording test
revealed the custom user-agent was missing from viewer commands. The corrected
recording and tab-switch paths now match both the core handler's user-agent and
Chromium arguments. The smoke fixture uses those full settings too.
Agent and workspace health checks passed, the gateway serves the new UI,
and shared CDP remains disabled. The previous release is retained for rollback.

The recording release passed an isolated real Linux browser test: a playable
WebM, expected HAR request and console message, nonempty ZIP and manifest,
and preservation of cookies, session storage, and both existing tabs. Fake
runtime tests also cover cross-workflow rejection and retryable partial stops.
The deployed UI check passed on an actual workflow-tool-created session:
Start/Stop recording, click-to-control tab switching, returning control, and
Fill width/Fit page all worked while retaining both existing tabs. The capture
was saved to `Workflow/rtslatency/browser-recordings/20260909T072455Z-2105749843`
and its manifest opened through the Files button. Builder view actions returned
`applied`; the brief switch toast was not captured visually during this check.

Browser is alongside the other workspace views (the current toolbar labels
this group **Pulse**), and is removed from **Setup**.
Its mode and connection settings remain behind the Browser panel’s gear button.


## Shared persistent Chrome (opt-in)

Set `AGENT_BROWSER_SHARED_PROFILE` to an absolute dedicated directory outside
release folders, for example `/data/video-studio/browser-profile`. Unset it to
retain isolated sessions. A filesystem root or relative path is rejected.

In shared headless mode, all agent session names map to `shared-browser`.
All signed-in users with access to a browser-enabled workflow can see the same
browser; write access is still required for input and recording. Everyone can
access the browser's logged-in accounts. There is no extra control locking;
users coordinate concurrent actions themselves. Workflow completion and idle
cleanup do not close the shared browser. An explicit browser close still closes
it for everyone, so agents should inspect existing tabs and avoid reset/close
or clearing storage without a user request.

The shared launch settings are defined once in `workspace/browserconfig` and
used by automation, viewer tab controls, recording, and the browser supervisor.
Shared mode keeps Chrome's native Linux/version user agent, `en-US`, a default
1280x720 viewport, and UTC timezone. It retains the existing AutomationControlled
flag. No third-party stealth plugin is installed; detection avoidance is not
guaranteed. Browser upgrades may change the fingerprint. Existing isolated
profiles are not merged into the new shared profile.

Install the user unit `deploy/aws-ec2/server/video-studio-browser.service` and
run `video-studio-browser` from the release's `bin` directory. It supervises
Chrome separately from the agent/workspace services and gracefully closes it
on service stop. Enable the unit for the user's default target to restart it
on boot. The rootless release build includes the supervisor binary.
The profile directory must remain on persistent storage across deployments.
Cookies and local storage were verified across a real Chrome restart; sites
can still expire sessions or require MFA. This is login-session persistence,
not a separately configured password manager.

Recordings are still saved under the workflow that starts them. A capture
started from another workflow must be stopped from its originating workflow;
this avoids exposing its workspace files through the shared-browser viewer.


### RTS shared-profile rollout (2026-09-09)

Enabled on RTS in `releases/shared-browser-20260909` with shared changes
`66c9a7e07`. Profile: `/data/video-studio/browser-profile`. The separate
`video-studio-browser` user service is enabled for the default target; agent,
workspace, gateway, and browser service health checks passed. The UI displays
**Shared browser · all users** and live frames without requiring a workflow
run to create the browser first. Shared-mode Linux recording/streaming tests
preserved cookies, session storage and both tabs. A separate real Chrome
restart retained a persistent cookie and local storage. Observed identity:
Linux HeadlessChrome 152.0.7928.2, en-US, UTC, 1280x720. Other deployments keep
isolated behavior until explicitly configured with a shared profile.


## Recording through chat

The existing `workspace_browser.agent_browser` tool supports Builder's bundled
`capture` command in managed headless mode (isolated or persistent shared):

```json
{"command":"capture","args":["status"],"session":"main"}
{"command":"capture","args":["start"],"session":"main"}
{"command":"capture","args":["stop"],"session":"main"}
```

Ask the agent, for example: “Record the browser, network and console while you
reproduce this issue, then save the recording.” The browser must already be
running with the intended page selected. The agent uses the same workspace
recording endpoint as the UI, so the Browser view's existing status polling
reflects chat start/stop operations. No new tool or CLI binary is needed.

The managed handler derives the owning workflow from trusted session settings
and sends its current folder permissions to the workspace service. Output normally
lands in `Workflow/<name>/browser-recordings/<capture-id>/`; a step with narrower
write access uses its authorized working directory's `browser-recordings/`.
The response supplies the actual directory and files. Read-only sessions may
inspect authorized recording status but cannot start or stop a capture. Another
workflow cannot stop an active recording or retrieve its paths. A completed
recording does not prevent another workflow from starting a new capture.

Start begins video and HAR capture and clears console/error buffers. Stop exports
`video.webm`, `network.har`, `console.json`, `errors.json`, `manifest.json`, and
`capture.zip`. HAR response bodies are excluded. Video captures the page active
at start; this does not promise recording all tabs. Console/error output is a
buffer export, not an unlimited log stream.

Start/stop are idempotent. After a timeout, query status before retrying. A partial
stop returns errors and may leave `recording=true`; inspect the result and retry
stop when necessary. Stop captures started for the task even if reproduction
fails, but do not automatically stop a recording that was already running. Never
mix a bundled capture with separate native `record`/HAR start/stop commands.
Stopping capture preserves the browser and sign-ins.

This extension belongs to Builder's handler and is documented in the tool schema,
Builder browser skill, and browser guidance. Upstream `agent-browser skills` does
not define it. Native `record` remains video-only; external CDP currently uses
its existing separate video/HAR/console commands instead of bundled `capture`.


## Synthetic microphone and camera

Managed headless browser launches (including the shared supervisor, tool commands,
UI tab controls and recording) include these Chrome flags through the central
`workspace/browserconfig.HeadlessArgs()` helper:

- `--use-fake-device-for-media-stream`
- `--use-fake-ui-for-media-stream`

These provide synthetic media devices and automatic media permission handling,
so server-side flows that require a microphone can obtain a stream. They do not
supply the user's voice or meaningful spoken dialogue. External CDP Chrome keeps
its own launch configuration. An already running Chrome needs one graceful
restart to apply the flags; the persistent profile is retained, while in-memory
page state and ongoing calls may need to be resumed.

The Builder agent-browser skill explains how to verify getUserMedia and the
application outcome without switching to an unrelated Playwright harness. A
microphone permission success alone is not evidence that an RTS simulation started.

References: [agent-browser launch options](https://agent-browser.dev/configuration),
[Chromium media switches](https://chromium.googlesource.com/chromium/src/+/main/media/base/media_switches.cc).
