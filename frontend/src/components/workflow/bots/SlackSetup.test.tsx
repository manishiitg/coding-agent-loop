// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "../../ui/tooltip";
import type { SlackConnection } from "../../../services/api-types";
import type { WorkflowRoute } from "./types";

vi.mock("../../admin/SlackAdminPanel", () => ({
  SharedSlackBotSettings: () => <div data-testid="shared-bot-settings" />,
}));

vi.mock("../../../hooks/useCanWriteWorkflow", () => ({
  READ_ONLY_TITLE: "read-only",
  useCanWriteWorkflow: () => true,
}));

import { SlackSetup } from "./SlackSetup";

const sharedBot: SlackConnection = { id: "slack_001", display_name: "AgentWorks", enabled: true, configured: true, is_default: true };
const ownBot: SlackConnection = { id: "slack_own", display_name: "Support bot", enabled: true, configured: true, is_default: false, workspace_path: "Workflow/support" };

function makeBots(overrides: { own?: SlackConnection | null; routes?: WorkflowRoute[]; canManageSlackDefault?: boolean } = {}) {
  const noop = () => {};
  const asyncNoop = async () => null;
  return {
    readOnly: false,
    workflowId: "wf-support",
    slackOriginal: { enabled: true, bot_mode: true, connections: [sharedBot, ...(overrides.own ? [overrides.own] : [])] },
    loadSlack: asyncNoop,
    canManageSlackDefault: overrides.canManageSlackDefault ?? false,
    slackLoading: false,
    slackError: null,
    slackSuccess: null,
    canManageWorkflowSlack: true,
    hasProfileTarget: false,
    slackSelection: { own: overrides.own ?? null, effective: overrides.own ?? sharedBot, selectionId: overrides.own?.id ?? "" },
    slackConnName: "", setSlackConnName: noop, slackConnBot: "", setSlackConnBot: noop, slackConnApp: "", setSlackConnApp: noop,
    slackConnEnabled: true, setSlackConnEnabled: noop, slackConnSaving: false, slackConnTesting: false, slackConnTestResult: null,
    slackConnConfirmDelete: false, slackConnHasChanges: false,
    saveWorkflowSlackConnection: asyncNoop, testWorkflowSlackConnection: async () => {}, removeWorkflowSlackConnection: async () => false,
    workflowRoutes: overrides.routes ?? [], routeError: null,
    newSlackChannel: "", setNewSlackChannel: noop, addSlackRoute: noop,
    routeSaving: null, myRoutes: overrides.routes ?? [], addError: {}, setAddError: noop,
    expandedChip: null, setExpandedChip: noop, removeRoute: async () => {}, updateRoute: async () => {},
  } as unknown as React.ComponentProps<typeof SlackSetup>["bots"];
}

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
});

afterEach(() => {
  document.body.innerHTML = "";
});

async function render(bots: React.ComponentProps<typeof SlackSetup>["bots"]) {
  const container = document.createElement("div");
  document.body.append(container);
  await act(async () => createRoot(container).render(<TooltipProvider><SlackSetup bots={bots} /></TooltipProvider>));
  return container;
}

function radio(host: HTMLElement, label: RegExp) {
  const option = Array.from(host.querySelectorAll("label")).find(el => label.test(el.textContent || ""));
  return option?.querySelector('input[type="radio"]') as HTMLInputElement;
}

it("asks one question and defaults a fresh workflow to its own bot", async () => {
  const host = await render(makeBots());
  expect(host.textContent).toContain("Who answers for this workflow in Slack?");
  expect(radio(host, /Its own bot/).checked).toBe(true);
  expect(host.textContent).toContain("Save bot");
  expect(host.textContent).not.toContain("Add channel");
});

it("shows where to get each Slack token next to its field", async () => {
  const host = await render(makeBots());
  const botField = host.querySelector<HTMLInputElement>('input[placeholder="xoxb-..."]')?.parentElement?.parentElement;
  const appField = host.querySelector<HTMLInputElement>('input[placeholder="xapp-..."]')?.parentElement?.parentElement;
  expect(botField?.textContent).toContain("OAuth & Permissions → OAuth Tokens");
  expect(botField?.textContent).toContain("Bot User OAuth Token");
  expect(appField?.textContent).toContain("Basic Information → App-Level Tokens");
  expect(appField?.textContent).toContain("connections:write");
  expect(botField?.querySelector('a')?.getAttribute('href')).toBe('https://api.slack.com/apps');
  expect(appField?.querySelector('a')?.getAttribute('href')).toBe('https://api.slack.com/apps');
  expect(host.querySelector('summary')?.textContent).toContain('Where to get Slack tokens');
});

it("shows required scopes, optional scopes, and bot events without opening setup details", async () => {
  const host = await render(makeBots());
  const checklist = host.querySelector('[aria-label="Slack permissions and events"]')!;
  const required = Array.from(checklist.querySelectorAll('ul code')).map(item => item.textContent);
  expect(required).toEqual([
    'app_mentions:read', 'channels:history', 'groups:history', 'channels:read', 'groups:read',
    'chat:write', 'reactions:write', 'users:read', 'users:read.email',
  ]);
  expect(checklist.textContent).toContain('Optional bot scopes: files:read');
  expect(checklist.textContent).toContain('chat:write.public');
  expect(checklist.textContent).toContain('connections:write');
  expect(checklist.textContent).toContain('app_mention message.channels message.groups');
  expect(checklist.textContent).toContain('Reinstall the Slack app after changing scopes');
});

it("shows a configured own bot as a summary with no channel setup", async () => {
  const host = await render(makeBots({ own: ownBot }));
  expect(radio(host, /Its own bot/).checked).toBe(true);
  expect(host.textContent).toContain("Support bot");
  expect(host.textContent).toContain("Ready");
  expect(host.textContent).toContain("/invite @Support bot");
  expect(host.textContent).not.toContain("Save bot");
});

it("opens on the shared bot when the workflow already has channels", async () => {
  const routes: WorkflowRoute[] = [{ kind: "slack", key: "C0123456789", current_target: true }];
  const host = await render(makeBots({ routes }));
  expect(radio(host, /Shared bot/).checked).toBe(true);
  expect(host.textContent).toContain("Add channel");
  expect(host.textContent).toContain("C0123456789");
});

it("warns that an own bot keeps answering when the shared bot is picked", async () => {
  const host = await render(makeBots({ own: ownBot }));
  await act(async () => {
    radio(host, /Shared bot/).click();
  });
  expect(host.textContent).toContain("still answers wherever it's invited");
  expect(host.textContent).toContain("Remove own bot");
});

it("keeps shared bot settings for admins only", async () => {
  const routes: WorkflowRoute[] = [{ kind: "slack", key: "C0123456789", current_target: true }];
  const member = await render(makeBots({ routes }));
  expect(member.querySelector('[data-testid="shared-bot-settings"]')).toBeNull();
  const admin = await render(makeBots({ routes, canManageSlackDefault: true }));
  expect(admin.querySelector('[data-testid="shared-bot-settings"]')).not.toBeNull();
});
