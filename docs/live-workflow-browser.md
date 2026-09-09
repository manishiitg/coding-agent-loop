# Live browser in workflows

The workflow Browser automation tab shows the managed headless browser for that
workflow. It discovers sessions automatically when `agent_browser open` runs.
Select a browser session to watch its active tab. Browser settings remain below
the viewer. Closed, completed, or reaped sessions disappear from the list.

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
