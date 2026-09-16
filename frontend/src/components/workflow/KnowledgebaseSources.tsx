import { useCallback, useEffect, useId, useState } from "react";
import { workflowManifestApi } from "../../services/api";
import type {
  KnowledgebaseSource,
  KnowledgebaseSourceStatus,
} from "../../services/api-types";
import { useCanWriteWorkflow } from "../../hooks/useCanWriteWorkflow";
import { BookOpen, LockKeyhole } from "lucide-react";
import { WORKFLOW_KNOWLEDGE_SOURCES_REFRESH_EVENT } from "./workflowEvents";

/** Grantor-side control: which other workflow IDs may write into THIS
 * workflow's knowledgebase/notes/. A consumer's "write" knowledgebase_source
 * does nothing until its workflow ID appears here. */
function KBWriteGrants({
  workspacePath,
  canWrite,
}: {
  workspacePath: string;
  canWrite: boolean;
}) {
  const [loading, setLoading] = useState(true);
  const [grants, setGrants] = useState<string[]>([]);
  const [newID, setNewID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await workflowManifestApi.getWorkflowManifest(workspacePath);
      setGrants(result.manifest.access?.allowed_kb_writers || []);
    } catch (e) {
      setError(sourceError(e, "Unable to load KB write grants"));
    } finally {
      setLoading(false);
    }
  }, [workspacePath]);
  useEffect(() => {
    void load();
  }, [load]);
  const save = async (next: string[]) => {
    setBusy(true);
    setError("");
    try {
      await workflowManifestApi.updateWorkflowManifest({
        workspace_path: workspacePath,
        kb_write_grants: next,
      });
      setGrants(next);
      setNewID("");
    } catch (e) {
      setError(sourceError(e, "Unable to update KB write grants"));
    } finally {
      setBusy(false);
    }
  };
  if (!canWrite) return null;
  return (
    <div className="space-y-2 rounded-lg border border-dashed p-3">
      <h4 className="text-sm font-semibold">
        KB write grants (this workflow's own knowledgebase)
      </h4>
      <p className="text-muted-foreground">
        Workflow IDs permitted to write into this workflow's
        knowledgebase/notes/ via their own read-write attachment. Does not
        affect read sharing.
      </p>
      {loading && (
        <p role="status" className="text-muted-foreground">
          Loading…
        </p>
      )}
      {!loading && grants.length === 0 && (
        <p className="text-muted-foreground">No external write access granted.</p>
      )}
      {grants.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {grants.map((id) => (
            <li
              key={id}
              className="inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5"
            >
              <span className="font-mono">{id}</span>
              <button
                type="button"
                disabled={busy}
                onClick={() => void save(grants.filter((g) => g !== id))}
                aria-label={`Revoke write access for ${id}`}
                className="text-muted-foreground hover:text-foreground"
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      )}
      <form
        className="flex flex-wrap items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          const id = newID.trim();
          if (id && !grants.includes(id)) void save([...grants, id]);
        }}
      >
        <label className="grid gap-1">
          Workflow ID
          <input
            value={newID}
            onChange={(e) => setNewID(e.target.value)}
            placeholder="workflow-id"
            className="rounded border bg-background p-1.5"
          />
        </label>
        <button
          type="submit"
          disabled={busy || !newID.trim()}
          className="rounded border px-2 py-1.5 disabled:opacity-50"
        >
          Grant
        </button>
      </form>
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

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
  const [accessLevel, setAccessLevel] = useState<"read" | "write">("read");
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
    if (variant !== "folders" || !canWrite || !sourcesLoaded) return;
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
      setAccessLevel("read");
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
      {variant === "folders" && (
        <>
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
            available directly. A read-write attachment can also contribute
            to the source's notes/ once the source grants this workflow
            write access — see “KB write grants” below to grant it the
            other way.
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
                      {source.access === "write" ? "Read-write (notes/)" : "Read only"}
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
                    <p
                      role="status"
                      className="text-amber-700 dark:text-amber-300"
                    >
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
                          void save(
                            refs.filter((s) => s.alias !== source.alias),
                          )
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
              steps with knowledge-base read access. A read-write source is
              additionally writable in its notes/ folder by steps with
              knowledge-base write access, once granted.
            </p>
          )}
        </>
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
                  {source.available
                    ? source.access === "write"
                      ? "Read-write"
                      : "Read only"
                    : "Unavailable"}
                </option>
              ))}
            </select>
          </div>
          <p className="text-muted-foreground">
            {selected
              ? sources.find((s) => s.alias === selected)?.access === "write"
                ? `Viewing shared knowledge from ${sources.find((s) => s.alias === selected)?.label || selected}. This workflow may also contribute to its notes/.`
                : `Viewing shared knowledge from ${sources.find((s) => s.alias === selected)?.label || selected}. Read only; maintained in the source workflow.`
              : "Viewing this workflow’s local knowledge."}
          </p>
          <p className="text-[11px] text-muted-foreground">
            Manage shared knowledge bases in Setup → Attached folders.
          </p>
          {sources.find((s) => s.alias === selected)?.available === false && (
            <p role="status" className="text-amber-700 dark:text-amber-300">
              Unavailable ·{" "}
              {sources.find((s) => s.alias === selected)?.reason ||
                "Source unavailable"}
            </p>
          )}
        </div>
      )}
      {variant === "folders" && editing && (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void save([
              ...refs,
              { workflow_id: sourceID, alias, access: accessLevel },
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
          <label className="grid gap-1">
            Access
            <select
              value={accessLevel}
              onChange={(e) =>
                setAccessLevel(e.target.value as "read" | "write")
              }
              className="rounded border bg-background p-1.5"
            >
              <option value="read">Read only</option>
              <option value="write">Read-write (notes/ only)</option>
            </select>
          </label>
          <button
            disabled={busy || !sourceID || !alias}
            className="rounded border px-2 py-1.5 disabled:opacity-50"
          >
            {accessLevel === "write" ? "Attach read-write" : "Attach read-only"}
          </button>
          <button
            type="button"
            onClick={() => {
              setEditing(false);
              setAccessLevel("read");
            }}
            className="px-2 py-1.5"
          >
            Cancel
          </button>
          {accessLevel === "write" && (
            <p className="basis-full text-[11px] text-muted-foreground">
              Takes effect only once the source workflow's owner grants this
              workflow's ID in its own KB write grants — otherwise this
              source resolves as unavailable.
            </p>
          )}
        </form>
      )}
      {variant === "folders" && (
        <KBWriteGrants workspacePath={workspacePath} canWrite={canWrite} />
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
