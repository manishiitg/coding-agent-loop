import { useEffect, useState } from "react";
import { agentApi } from "../../../services/api";
import {
  WORKFLOW_DECISIONS_REFRESH_EVENT,
  WORKFLOW_LOG_REFRESH_EVENT,
} from "../workflowEvents";

// The primary Pulse button remains mounted when the decision panel is closed.
// Refresh on saved decisions/chat receipts and periodically for background work.
export function usePendingDecisionCount(workspacePath?: string | null): number {
  const [snapshot, setSnapshot] = useState({ workspace: "", count: 0 });
  useEffect(() => {
    if (!workspacePath) return;
    let disposed = false,
      generation = 0;
    const refresh = async () => {
      const request = ++generation;
      try {
        const response = await agentApi.listReportHumanInputs(
          workspacePath,
          "pending",
        );
        if (disposed || request !== generation || !response.success) return;
        setSnapshot({
          workspace: workspacePath,
          count: (response.inputs || []).filter(
            (input) =>
              input.status === "pending" &&
              input.workspace_path === workspacePath,
          ).length,
        });
      } catch {
        /* Preserve the last confirmed count during a transient failure. */
      }
    };
    const refreshVisible = () => {
      if (document.visibilityState !== "hidden") void refresh();
    };
    const refreshDecision = (event: Event) => {
      const workspace = (event as CustomEvent<{ workspacePath?: string }>)
        .detail?.workspacePath;
      if (!workspace || workspace === workspacePath) void refresh();
    };
    void refresh();
    const interval = window.setInterval(refreshVisible, 30_000);
    window.addEventListener(WORKFLOW_LOG_REFRESH_EVENT, refreshDecision);
    window.addEventListener(WORKFLOW_DECISIONS_REFRESH_EVENT, refreshDecision);
    window.addEventListener("focus", refreshVisible);
    document.addEventListener("visibilitychange", refreshVisible);
    return () => {
      disposed = true;
      window.clearInterval(interval);
      window.removeEventListener(WORKFLOW_LOG_REFRESH_EVENT, refreshDecision);
      window.removeEventListener(
        WORKFLOW_DECISIONS_REFRESH_EVENT,
        refreshDecision,
      );
      window.removeEventListener("focus", refreshVisible);
      document.removeEventListener("visibilitychange", refreshVisible);
    };
  }, [workspacePath]);
  return snapshot.workspace === workspacePath ? snapshot.count : 0;
}
