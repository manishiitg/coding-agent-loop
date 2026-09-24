import { useEffect, useState } from "react";
import { agentApi } from "../../../services/api";
import { useLiveRefetch } from "../../../hooks/useLiveRefetch";
import {
  WORKFLOW_DECISIONS_REFRESH_EVENT,
  WORKFLOW_LOG_REFRESH_EVENT,
} from "../workflowEvents";

// The primary Pulse button remains mounted when the decision panel is closed.
// Refresh on saved decisions/chat receipts and periodically for background work.
export function usePendingDecisionCount(workspacePath?: string | null): number {
  const [snapshot, setSnapshot] = useState({ workspace: "", count: 0 });
  // Live notices bump this to refetch; 30s polling only if the feed is down.
  const [liveTick, setLiveTick] = useState(0);
  useLiveRefetch(() => setLiveTick((tick) => tick + 1), {
    kinds: ["human_inputs"],
    workflow: workspacePath ?? null,
    fallbackMs: 30_000,
    enabled: !!workspacePath,
  });
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
    const refreshDecision = (event: Event) => {
      const workspace = (event as CustomEvent<{ workspacePath?: string }>)
        .detail?.workspacePath;
      if (!workspace || workspace === workspacePath) void refresh();
    };
    void refresh();
    window.addEventListener(WORKFLOW_LOG_REFRESH_EVENT, refreshDecision);
    window.addEventListener(WORKFLOW_DECISIONS_REFRESH_EVENT, refreshDecision);
    return () => {
      disposed = true;
      window.removeEventListener(WORKFLOW_LOG_REFRESH_EVENT, refreshDecision);
      window.removeEventListener(
        WORKFLOW_DECISIONS_REFRESH_EVENT,
        refreshDecision,
      );
    };
  }, [workspacePath, liveTick]);
  return snapshot.workspace === workspacePath ? snapshot.count : 0;
}
