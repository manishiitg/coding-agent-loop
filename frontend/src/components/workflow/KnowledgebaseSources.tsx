import { useCallback, useEffect, useState } from "react";
import { workflowManifestApi } from "../../services/api";
import type {
  KnowledgebaseSource,
  KnowledgebaseSourceStatus,
} from "../../services/api-types";
import { useCanWriteWorkflow } from "../../hooks/useCanWriteWorkflow";

function sourceError(error: unknown, fallback: string) {
  const body = (error as { response?: { data?: unknown } })?.response?.data;
  return typeof body === "string" && body.trim()
    ? body.trim()
    : error instanceof Error
      ? error.message
      : fallback;
}

export function KnowledgebaseSources({
  workspacePath,
  selected,
  onSelect,
}: {
  workspacePath: string;
  selected: string;
  onSelect: (alias: string) => void;
}) {
  const canWrite = useCanWriteWorkflow(workspacePath);
  const [sources, setSources] = useState<KnowledgebaseSourceStatus[]>([]);
  const [candidates, setCandidates] = useState<
    Array<{ id: string; label: string }>
  >([]);
  const [editing, setEditing] = useState(false);
  const [sourceID, setSourceID] = useState("");
  const [alias, setAlias] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const refresh = useCallback(async () => {
    const result =
      await workflowManifestApi.getKnowledgebaseSources(workspacePath);
    setSources(result.sources || []);
  }, [workspacePath]);
  useEffect(() => {
    let active = true;
    workflowManifestApi
      .getKnowledgebaseSources(workspacePath)
      .then((result) => {
        if (active) setSources(result.sources || []);
      })
      .catch((e) => {
        if (active) setError(e.message || "Knowledge sources unavailable");
      });
    return () => {
      active = false;
    };
  }, [workspacePath]);
  const open = async () => {
    setBusy(true);
    setError("");
    try {
      const result = await workflowManifestApi.listWorkflowManifests();
      setCandidates(
        result.workflows
          .filter(
            (w) =>
              w.workspace_path !== workspacePath &&
              !sources.some((s) => s.workflow_id === w.manifest.id),
          )
          .map((w) => ({ id: w.manifest.id, label: w.manifest.label })),
      );
      setEditing(true);
    } catch (e) {
      setError(sourceError(e, "Unable to list workflows"));
    } finally {
      setBusy(false);
    }
  };
  const save = async (next: KnowledgebaseSource[]) => {
    if (!canWrite) return;
    setBusy(true);
    setError("");
    try {
      await workflowManifestApi.updateWorkflowManifest({
        workspace_path: workspacePath,
        knowledgebase_sources: next,
      });
      if (selected && !next.some((s) => s.alias === selected)) onSelect("");
      await refresh();
      setEditing(false);
      setAlias("");
      setSourceID("");
    } catch (e) {
      setError(sourceError(e, "Unable to update knowledge sources"));
    } finally {
      setBusy(false);
    }
  };
  const refs = sources.map(({ workflow_id, alias, access }) => ({
    workflow_id,
    alias,
    access,
  }));
  return (
    <section
      aria-label="Knowledge sources"
      className="space-y-2 border-b p-3 text-xs"
    >
      <div className="flex flex-wrap items-center gap-2">
        <label htmlFor="kb-source-select" className="font-medium">
          Knowledge source
        </label>
        <select
          id="kb-source-select"
          value={selected}
          onChange={(e) => onSelect(e.target.value)}
          className="min-w-0 rounded border bg-background p-1.5"
        >
          <option value="">Local knowledge</option>
          {sources.map((source) => (
            <option key={source.alias} value={source.alias}>
              {source.label || source.alias} ({source.alias}) ·{" "}
              {source.available ? "Read only" : "Unavailable"}
            </option>
          ))}
        </select>
        {canWrite && (
          <button
            type="button"
            disabled={busy}
            onClick={open}
            className="rounded border px-2 py-1.5 disabled:opacity-50"
          >
            Attach knowledge
          </button>
        )}
        {canWrite && selected && (
          <button
            type="button"
            disabled={busy}
            onClick={() => save(refs.filter((s) => s.alias !== selected))}
            className="rounded border px-2 py-1.5 disabled:opacity-50"
          >
            Detach
          </button>
        )}
      </div>
      {sources
        .filter((s) => !s.available)
        .map((s) => (
          <p key={s.alias} role="status" className="text-amber-600">
            {s.alias}: {s.reason || "Source unavailable"}
          </p>
        ))}
      {selected && (
        <p className="text-muted-foreground">
          Shared from another workflow. Updates are maintained in the source
          workflow.
        </p>
      )}
      {editing && (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void save([
              ...refs,
              { workflow_id: sourceID, alias, access: "read" },
            ]);
          }}
        >
          <label className="grid gap-1">
            Source workflow
            <select
              required
              value={sourceID}
              onChange={(e) => setSourceID(e.target.value)}
              className="rounded border bg-background p-1.5"
            >
              <option value="">Choose workflow</option>
              {candidates.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.label}
                </option>
              ))}
            </select>
          </label>
          <label className="grid gap-1">
            Alias
            <input
              required
              pattern="[a-z][a-z0-9_]{0,47}"
              maxLength={48}
              value={alias}
              onChange={(e) => setAlias(e.target.value)}
              placeholder="rts"
              className="rounded border bg-background p-1.5"
            />
          </label>
          <button
            disabled={busy || !sourceID || !alias}
            className="rounded border px-2 py-1.5 disabled:opacity-50"
          >
            Attach read-only
          </button>
          <button
            type="button"
            onClick={() => setEditing(false)}
            className="px-2 py-1.5"
          >
            Cancel
          </button>
        </form>
      )}
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
    </section>
  );
}
