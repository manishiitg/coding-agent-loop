import { useCallback, useEffect, useId, useState } from "react";
import { workflowManifestApi } from "../../services/api";
import type {
  KnowledgebaseSource,
  KnowledgebaseSourceStatus,
} from "../../services/api-types";
import { useCanWriteWorkflow } from "../../hooks/useCanWriteWorkflow";
import { BookOpen, LockKeyhole } from "lucide-react";
import { WORKFLOW_KNOWLEDGE_SOURCES_REFRESH_EVENT } from "./workflowEvents";

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
  selected = "",
  onSelect,
  variant = "knowledge",
}: {
  workspacePath: string;
  selected?: string;
  onSelect?: (alias: string) => void;
  variant?: "knowledge" | "folders";
}) {
  const canWrite = useCanWriteWorkflow(workspacePath);
  const instanceID = useId();
  const selectID = `${instanceID}-kb-source`;
  const [loading, setLoading] = useState(true);
  const [reloadKey, setReloadKey] = useState(0);
  const [sourcesLoaded, setSourcesLoaded] = useState(false);
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
    let request = 0;
    const reload = async () => {
      const current = ++request;
      setLoading(true);
      setError("");
      try {
        const result =
          await workflowManifestApi.getKnowledgebaseSources(workspacePath);
        if (active && current === request) {
          setSources(result.sources || []);
          setSourcesLoaded(true);
        }
      } catch (e) {
        if (active && current === request) {
          setSourcesLoaded(false);
          setError(sourceError(e, "Knowledge sources unavailable"));
        }
      } finally {
        if (active && current === request) setLoading(false);
      }
    };
    const changed = (event: Event) => {
      const detail = (
        event as CustomEvent<{ workspacePath: string; origin: string }>
      ).detail;
      if (
        detail?.workspacePath === workspacePath &&
        detail.origin !== instanceID
      )
        void reload();
    };
    setSources([]);
    setSourcesLoaded(false);
    void reload();
    window.addEventListener(WORKFLOW_KNOWLEDGE_SOURCES_REFRESH_EVENT, changed);
    return () => {
      active = false;
      window.removeEventListener(
        WORKFLOW_KNOWLEDGE_SOURCES_REFRESH_EVENT,
        changed,
      );
    };
  }, [workspacePath, instanceID, reloadKey]);
  useEffect(() => {
    if (
      sourcesLoaded &&
      !loading &&
      selected &&
      !sources.some((s) => s.alias === selected)
    )
      onSelect?.("");
  }, [sources, sourcesLoaded, loading, selected, onSelect]);
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
    if (!canWrite || !sourcesLoaded) return;
    setBusy(true);
    setError("");
    try {
      await workflowManifestApi.updateWorkflowManifest({
        workspace_path: workspacePath,
        knowledgebase_sources: next,
      });
      if (selected && !next.some((s) => s.alias === selected)) onSelect?.("");
      window.dispatchEvent(
        new CustomEvent(WORKFLOW_KNOWLEDGE_SOURCES_REFRESH_EVENT, {
          detail: { workspacePath, origin: instanceID },
        }),
      );
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
      className={
        variant === "folders"
          ? "space-y-3 rounded-lg border border-border p-4 text-xs"
          : "max-h-[50%] shrink-0 space-y-3 overflow-y-auto border-b p-3 text-xs"
      }
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <BookOpen aria-hidden="true" className="h-4 w-4 text-primary" />
          <h3 className="text-sm font-semibold">Shared knowledge bases</h3>
          {!loading && sourcesLoaded && (
            <span className="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
              {sources.length} attached
            </span>
          )}
        </div>
        {canWrite && (
          <button
            type="button"
            disabled={busy || loading || !sourcesLoaded}
            onClick={open}
            className="rounded border px-2 py-1.5 disabled:opacity-50"
          >
            Attach knowledge
          </button>
        )}
      </div>
      <p className="text-muted-foreground">
        Read context and notes from other workflows. Source updates are
        available directly; contributions stay in this workflow’s local
        knowledge base.
      </p>
      {loading && (
        <p role="status" className="text-muted-foreground">
          Loading shared knowledge…
        </p>
      )}
      {!loading && !error && sources.length === 0 && (
        <p className="rounded border border-dashed p-3 text-muted-foreground">
          No shared knowledge bases attached.
        </p>
      )}
      {sources.length > 0 && (
        <ul
          className="grid gap-2 lg:grid-cols-2"
          aria-label="Attached knowledge bases"
        >
          {sources.map((source) => (
            <li
              key={source.alias}
              className={`min-w-0 space-y-2 rounded-lg border p-3 ${selected === source.alias ? "border-primary/40 bg-primary/5" : "border-border bg-muted/20"}`}
            >
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="font-medium text-foreground">
                  {source.label || source.workflow_id}
                </span>
                <span className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] text-muted-foreground">
                  <LockKeyhole aria-hidden="true" className="h-3 w-3" />
                  Read only
                </span>
              </div>
              <div className="text-muted-foreground">
                Source workflow · Alias:{" "}
                <span className="font-medium text-foreground">
                  {source.alias}
                </span>
              </div>
              {source.workspace_path && (
                <div className="break-all text-muted-foreground">
                  {source.workspace_path}/knowledgebase/
                </div>
              )}
              <div className="flex flex-wrap items-center gap-1 text-muted-foreground">
                {source.available ? "Shell:" : "Shell when available:"}{" "}
                <code className="break-all rounded bg-muted px-1.5 py-0.5">
                  $WORKFLOW_KB_{source.alias.toUpperCase()}
                </code>
              </div>
              {!source.available && (
                <p role="status" className="text-amber-700 dark:text-amber-300">
                  Unavailable · {source.reason || "Source unavailable"}
                </p>
              )}
              <div className="flex flex-wrap items-center justify-between gap-2">
                {onSelect && (
                  <button
                    type="button"
                    onClick={() => onSelect(source.alias)}
                    aria-pressed={selected === source.alias}
                    className="rounded border px-2 py-1 text-foreground hover:bg-muted"
                  >
                    {selected === source.alias
                      ? "Viewing this source"
                      : "View knowledge"}
                  </button>
                )}
                {variant === "folders" && canWrite && (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() =>
                      void save(refs.filter((s) => s.alias !== source.alias))
                    }
                    aria-label={`Detach ${source.label || source.alias} knowledge base`}
                    className="rounded border px-2 py-1 disabled:opacity-50"
                  >
                    Detach
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {sources.length > 0 && (
        <p className="text-[11px] text-muted-foreground">
          Available sources can be read by the builder and reviewers, and by
          steps with knowledge-base read access. Shared attachments grant no
          write access.
        </p>
      )}
      {variant === "knowledge" && (
        <div className="space-y-2 rounded-lg bg-muted/30 p-2.5">
          <div className="flex flex-wrap items-center gap-2">
            <label htmlFor={selectID} className="font-medium">
              Browsing
            </label>
            <select
              id={selectID}
              value={selected}
              onChange={(e) => onSelect?.(e.target.value)}
              className="min-w-0 max-w-full rounded border bg-background p-1.5"
            >
              <option value="">Local knowledge · This workflow</option>
              {sources.map((source) => (
                <option key={source.alias} value={source.alias}>
                  {source.label || source.alias} ({source.alias}) ·{" "}
                  {source.available ? "Read only" : "Unavailable"}
                </option>
              ))}
            </select>
            {canWrite && selected && (
              <button
                type="button"
                disabled={busy}
                onClick={() =>
                  void save(refs.filter((s) => s.alias !== selected))
                }
                className="rounded border px-2 py-1.5 disabled:opacity-50"
              >
                Detach
              </button>
            )}
          </div>
          <p className="text-muted-foreground">
            {selected
              ? `Viewing shared knowledge from ${sources.find((s) => s.alias === selected)?.label || selected}. Read only; maintained in the source workflow.`
              : "Viewing this workflow’s local knowledge. Shared sources are listed above."}
          </p>
        </div>
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
        <div className="flex flex-wrap items-center gap-2">
          <p role="alert" className="text-destructive">
            {error}
          </p>
          {!sourcesLoaded && (
            <button
              type="button"
              disabled={loading}
              onClick={() => setReloadKey((key) => key + 1)}
              className="rounded border px-2 py-1.5"
            >
              Retry
            </button>
          )}
        </div>
      )}
    </section>
  );
}
