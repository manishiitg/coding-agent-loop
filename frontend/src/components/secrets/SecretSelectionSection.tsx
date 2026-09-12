import axios from 'axios';
import { useAuthStore } from '../../stores/useAuthStore';
import React, { useEffect, useMemo, useState } from 'react';
import { Checkbox } from '../ui/checkbox';
import { KeyRound, Globe, Plus, Trash2, Eye, EyeOff } from 'lucide-react';
import { useSecretsStore } from '../../stores';
import { secretsApi } from '../../api/secrets';
import { useCanWriteWorkflow, READ_ONLY_TITLE } from '../../hooks/useCanWriteWorkflow';

interface SecretSelectionSectionProps {
  selectedSecrets: string[];
  onSecretChange: (secrets: string[]) => void;
  selectedGlobalSecrets?: string[] | null; // null = all selected, [] = none selected
  onGlobalSecretChange?: (names: string[] | null) => void;
  workflowPath?: string;
  /** Lets the selector use an embedded side panel's remaining vertical space. */
  fillAvailableHeight?: boolean;
}

const isValidName = (name: string) => /^[A-Za-z_][A-Za-z0-9_]*$/.test(name);

export const SecretSelectionSection: React.FC<SecretSelectionSectionProps> = ({
  selectedSecrets,
  onSecretChange,
  selectedGlobalSecrets = [],
  onGlobalSecretChange,
  workflowPath,
  fillAvailableHeight = false,
}) => {
  const secrets = useSecretsStore((s) => s.secrets);
  const globalSecrets = useSecretsStore((s) => s.globalSecrets);
  const storedUserSecrets = useSecretsStore((s) => s.storedUserSecrets);
  const workflowSecretsByPath = useSecretsStore((s) => s.workflowSecretsByPath);
  const fetchGlobalSecrets = useSecretsStore((s) => s.fetchGlobalSecrets);
  const fetchStoredUserSecrets = useSecretsStore((s) => s.fetchStoredUserSecrets);
  const fetchWorkflowSecrets = useSecretsStore((s) => s.fetchWorkflowSecrets);
  const addWorkflowSecret = useSecretsStore((s) => s.addWorkflowSecret);
  const removeWorkflowSecret = useSecretsStore((s) => s.removeWorkflowSecret);
  // Workflow secrets are shared by everyone with access to the workflow:
  // owners add, delete and reveal them; a read-only user sees the names and
  // may run the workflow with them, but the server refuses reveal and every
  // mutation, so the controls disable here rather than fail on click.
  const canWrite = useCanWriteWorkflow(workflowPath?.trim() || undefined);
  const isAdmin = useAuthStore(state => state.user?.is_admin === true || (state.isMultiUserModeChecked && !state.isMultiUserMode));
  const [globalEditor, setGlobalEditor] = useState<string | null>(null);
  const [globalValue, setGlobalValue] = useState('');
  const [globalBusy, setGlobalBusy] = useState(false);
  const [globalStatus, setGlobalStatus] = useState('');
  const manageGlobal = async (action: 'promote' | 'set' | 'delete', name: string) => {
    if (!isAdmin || globalBusy) return;
    if (action === 'promote' && !confirm(`Make ${name} global? It will be available server-wide to all users and workflows. Its source workflow will use the global value.`)) return;
    if (action === 'delete' && !confirm(`Delete global secret ${name}? Workflows using it will no longer receive its value on new runs.`)) return;
    setGlobalBusy(true); setGlobalStatus('');
    try {
      if (action === 'promote') await secretsApi.promoteWorkflowSecret(workflowPath!.trim(), name);
      else if (action === 'set') await secretsApi.saveGlobalSecret(name, globalValue);
      else await secretsApi.deleteGlobalSecret(name);
      await fetchGlobalSecrets();
      if (workflowPath) await fetchWorkflowSecrets(workflowPath.trim());
      setGlobalEditor(null); setGlobalValue('');
      setGlobalStatus(action === 'delete' ? `Removed global secret ${name}.` : `${name} is global. Changes apply to new turns and runs.`);
    } catch (error) {
      setGlobalStatus(axios.isAxiosError(error) && typeof error.response?.data === 'string' ? error.response.data : 'Could not update global secret.');
    } finally { setGlobalBusy(false); }
  };

  const [workflowSecretName, setWorkflowSecretName] = useState('');
  const [workflowSecretValue, setWorkflowSecretValue] = useState('');
  const [workflowSecretError, setWorkflowSecretError] = useState<string | null>(null);
  const [savingWorkflowSecret, setSavingWorkflowSecret] = useState(false);
  // Revealed automation-secret values, decrypted on demand and dropped again
  // on hide so plaintext never sits in state longer than it is on screen.
  const [revealedValues, setRevealedValues] = useState<Record<string, string>>({});
  const [revealingName, setRevealingName] = useState<string | null>(null);

  const toggleReveal = async (secret: { name: string; encrypted_value?: string }) => {
    if (revealedValues[secret.name] !== undefined) {
      setRevealedValues((current) => {
        const next = { ...current };
        delete next[secret.name];
        return next;
      });
      return;
    }
    if (!secret.encrypted_value) return;
    setRevealingName(secret.name);
    try {
      const { value } = await secretsApi.decrypt(secret.encrypted_value, workflowPath?.trim() || undefined);
      setRevealedValues((current) => ({ ...current, [secret.name]: value }));
    } catch {
      setWorkflowSecretError(`Could not read the value of ${secret.name}.`);
    } finally {
      setRevealingName(null);
    }
  };

  const normalizedWorkflowPath = workflowPath?.trim() || '';
  const workflowSecrets = normalizedWorkflowPath
    ? workflowSecretsByPath[normalizedWorkflowPath] || []
    : [];

  useEffect(() => {
    if (globalSecrets.length === 0) {
      fetchGlobalSecrets();
    }
    fetchStoredUserSecrets();
  }, [fetchGlobalSecrets, fetchStoredUserSecrets, globalSecrets.length]);

  useEffect(() => {
    if (normalizedWorkflowPath) {
      fetchWorkflowSecrets(normalizedWorkflowPath);
    }
  }, [normalizedWorkflowPath, fetchWorkflowSecrets]);

  const toggleSecretName = (name: string, legacyId?: string) => {
    const isSelected = selectedSecrets.includes(name) || (!!legacyId && selectedSecrets.includes(legacyId));
    if (isSelected) {
      onSecretChange(selectedSecrets.filter(s => s !== name && s !== legacyId));
    } else {
      onSecretChange([...selectedSecrets, name]);
    }
  };

  const selectedSecretNames = useMemo(() => {
    const names = new Set<string>();
    for (const selected of selectedSecrets) {
      const byId = secrets.find((s) => s.id === selected);
      names.add(byId?.name || selected);
    }
    return names;
  }, [selectedSecrets, secrets]);

  const sharedSecrets = useMemo(() => {
    const byName = new Map<string, { name: string; legacyId?: string; local: boolean; serverStored: boolean }>();
    for (const secret of storedUserSecrets) {
      byName.set(secret.name, {
        name: secret.name,
        local: false,
        serverStored: true,
      });
    }
    for (const secret of secrets) {
      const existing = byName.get(secret.name);
      byName.set(secret.name, {
        name: secret.name,
        legacyId: secret.id,
        local: true,
        serverStored: existing?.serverStored ?? false,
      });
    }
    return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [secrets, storedUserSecrets]);

  const toggleGlobal = (name: string) => {
    if (!onGlobalSecretChange) return;
    const attachedByName = selectedSecretNames.has(name);
    if (attachedByName) onSecretChange(selectedSecrets.filter(selected => selected !== name));
    const isSelected = selectedGlobalSecrets === null || selectedGlobalSecrets.includes(name);
    if (isSelected) {
      const remaining = (selectedGlobalSecrets ?? globalSecrets.map(g => g.name)).filter(n => n !== name);
      onGlobalSecretChange(remaining);
    } else if (!attachedByName) {
      const next = [...(selectedGlobalSecrets ?? []), name];
      onGlobalSecretChange(next.length === globalSecrets.length ? null : next);
    }
  };

  const handleSaveWorkflowSecret = async () => {
    if (!normalizedWorkflowPath) return;
    setWorkflowSecretError(null);
    const trimmedName = workflowSecretName.trim().toUpperCase();
    if (!trimmedName) {
      setWorkflowSecretError('Name is required');
      return;
    }
    if (!isValidName(trimmedName)) {
      setWorkflowSecretError('Name must start with a letter or underscore and contain only letters, numbers, and underscores');
      return;
    }
    if (!workflowSecretValue) {
      setWorkflowSecretError('Value is required');
      return;
    }

    setSavingWorkflowSecret(true);
    try {
      const { encrypted } = await secretsApi.encrypt(workflowSecretValue);
      await addWorkflowSecret(normalizedWorkflowPath, trimmedName, encrypted);
      if (!selectedSecretNames.has(trimmedName)) {
        onSecretChange([...selectedSecrets, trimmedName]);
      }
      setWorkflowSecretName('');
      setWorkflowSecretValue('');
    } catch (err) {
      setWorkflowSecretError(err instanceof Error ? err.message : 'Failed to save automation secret');
    } finally {
      setSavingWorkflowSecret(false);
    }
  };

  const handleDeleteWorkflowSecret = async (name: string) => {
    if (!normalizedWorkflowPath) return;
    if (!confirm(`Delete automation secret "${name}"?`)) return;
    await removeWorkflowSecret(normalizedWorkflowPath, name);
    onSecretChange(selectedSecrets.filter(s => s !== name));
  };

  if (sharedSecrets.length === 0 && globalSecrets.length === 0 && workflowSecrets.length === 0 && !normalizedWorkflowPath) return null;

  const sortedWorkflowSecrets = [...workflowSecrets].sort((a, b) => a.name.localeCompare(b.name));

  return (
    <div className={fillAvailableHeight ? 'flex h-full min-h-0 flex-col gap-2' : 'space-y-2'}>
      <label className="block shrink-0 text-sm font-medium text-gray-900 dark:text-gray-100 flex items-center gap-2">
        <KeyRound className="w-4 h-4 text-amber-500" />
        Secrets
      </label>

      {normalizedWorkflowPath && canWrite && (
        <div className="shrink-0 space-y-2 rounded-md border border-amber-200 bg-amber-50 p-3 dark:border-amber-900/60 dark:bg-amber-950/20">
          <div className="flex items-center justify-between gap-2">
            <div className="min-w-0">
              <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Automation Secrets</div>
              <div className="truncate text-xs text-gray-500 dark:text-gray-400">{normalizedWorkflowPath}</div>
            </div>
            <span
              className="shrink-0 rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/60 dark:text-amber-300"
              title="Shared with everyone who has access to this workflow; only owners can change or reveal values"
            >
              Shared
            </span>
          </div>
          <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_auto]">
            <input
              type="text"
              value={workflowSecretName}
              onChange={(e) => setWorkflowSecretName(e.target.value.toUpperCase())}
              placeholder="SECRET_NAME"
              className="min-w-0 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 focus:outline-none focus:ring-1 focus:ring-amber-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
            />
            <input
              type="password"
              value={workflowSecretValue}
              onChange={(e) => setWorkflowSecretValue(e.target.value)}
              placeholder="Secret value"
              className="min-w-0 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-900 focus:outline-none focus:ring-1 focus:ring-amber-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
            />
            <button
              type="button"
              onClick={handleSaveWorkflowSecret}
              disabled={savingWorkflowSecret}
              className="inline-flex items-center justify-center gap-1.5 rounded-md bg-amber-600 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-amber-700 disabled:opacity-50"
            >
              <Plus className="h-4 w-4" />
              {savingWorkflowSecret ? 'Saving' : 'Save'}
            </button>
          </div>
          {workflowSecretError && <p className="text-xs text-red-600 dark:text-red-400">{workflowSecretError}</p>}
        </div>
      )}

      {globalStatus && <p role="status" className="text-xs text-muted-foreground">{globalStatus}</p>}
      {isAdmin && globalEditor !== null && <form className="flex shrink-0 flex-wrap items-center gap-2 rounded border border-border p-3" onSubmit={event => { event.preventDefault(); void manageGlobal('set', globalEditor); }}>
        <span className="text-xs">Update global {globalEditor}</span>
        <input type="password" aria-label="New global secret value" autoComplete="new-password" value={globalValue} onChange={event => setGlobalValue(event.target.value)} className="min-w-0 flex-1 rounded border border-border bg-background px-2 py-1 text-sm" />
        <button type="submit" disabled={globalBusy || !globalValue} className="text-xs disabled:opacity-40">Save</button>
        <button type="button" disabled={globalBusy} onClick={() => { setGlobalEditor(null); setGlobalValue(''); }} className="text-xs">Cancel</button>
      </form>}

      <div className={`border border-gray-200 dark:border-gray-700 rounded-md bg-white dark:bg-gray-800 ${fillAvailableHeight ? 'min-h-0 flex-1 overflow-y-auto' : ''}`}>
        {sortedWorkflowSecrets.map((secret) => (
          <div key={`workflow-${secret.name}`} className="flex items-center gap-2 p-3 border-b border-gray-200 dark:border-gray-700 last:border-b-0 hover:bg-gray-100 dark:hover:bg-gray-700">
            <Checkbox
              id={`workflow-secret-${secret.name}`}
              checked={selectedSecretNames.has(secret.name)}
              onCheckedChange={() => toggleSecretName(secret.name)}
            />
            <label htmlFor={`workflow-secret-${secret.name}`} className="flex-1 flex min-w-0 items-center gap-2 text-sm cursor-pointer select-none text-gray-900 dark:text-gray-100">
              <span className="min-w-0 flex-1 flex flex-col">
                <span className="min-w-0 truncate font-mono">{secret.name}</span>
                {revealedValues[secret.name] !== undefined && (
                  <span className="mt-0.5 break-all font-mono text-xs text-gray-600 dark:text-gray-300">{revealedValues[secret.name]}</span>
                )}
              </span>
              <span className="ml-auto shrink-0 text-[10px] px-1.5 py-0.5 rounded bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300">Automation</span>
            </label>
            {isAdmin && canWrite && <button type="button" disabled={globalBusy} onClick={() => void manageGlobal('promote', secret.name)} className="shrink-0 text-xs text-blue-600 disabled:opacity-40" aria-label={`Make ${secret.name} global`}>Make global</button>}
            <button
              type="button"
              onClick={() => { void toggleReveal(secret) }}
              disabled={!canWrite || !secret.encrypted_value || revealingName === secret.name}
              className="shrink-0 p-1 text-gray-400 transition-colors hover:text-gray-700 dark:hover:text-gray-200 disabled:cursor-not-allowed disabled:opacity-40"
              title={!canWrite ? READ_ONLY_TITLE : revealedValues[secret.name] !== undefined ? 'Hide value' : secret.encrypted_value ? 'Show value' : 'Value not available'}
            >
              {revealedValues[secret.name] !== undefined ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </button>
            <button
              type="button"
              onClick={() => handleDeleteWorkflowSecret(secret.name)}
              disabled={!canWrite}
              className="shrink-0 p-1 text-gray-400 transition-colors hover:text-red-600 dark:hover:text-red-400 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:text-gray-400"
              title={canWrite ? 'Delete automation secret' : READ_ONLY_TITLE}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </div>
        ))}

        {globalSecrets.map((gs) => (
          <div key={`global-${gs.name}`} className="flex items-center gap-2 p-3 border-b border-gray-200 dark:border-gray-700 last:border-b-0 hover:bg-gray-100 dark:hover:bg-gray-700">
            <Checkbox
              id={`global-secret-${gs.name}`}
              checked={selectedSecretNames.has(gs.name) || selectedGlobalSecrets === null || selectedGlobalSecrets.includes(gs.name)}
              onCheckedChange={() => toggleGlobal(gs.name)}
              disabled={!onGlobalSecretChange}
            />
            <label htmlFor={`global-secret-${gs.name}`} className="flex-1 flex items-center gap-2 text-sm cursor-pointer select-none">
              <Globe className="w-3.5 h-3.5 text-blue-500 flex-shrink-0" />
              <span className="font-mono">{gs.name}</span>
              <span className="ml-auto text-[10px] px-1.5 py-0.5 rounded bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-400">Global</span>
            </label>
            {isAdmin && gs.managed && <>
              <button type="button" disabled={globalBusy} onClick={() => { setGlobalEditor(gs.name); setGlobalValue(''); }} className="text-xs" aria-label={`Update global ${gs.name}`}>Update</button>
              <button type="button" disabled={globalBusy} onClick={() => void manageGlobal('delete', gs.name)} className="text-xs text-destructive" aria-label={`Delete global ${gs.name}`}>Remove</button>
            </>}
          </div>
        ))}

        {sharedSecrets.map((secret) => (
          <div key={`shared-${secret.name}`} className="flex items-center gap-2 p-3 border-b border-gray-200 dark:border-gray-700 last:border-b-0 hover:bg-gray-100 dark:hover:bg-gray-700">
            <Checkbox
              id={`secret-${secret.name}`}
              checked={selectedSecretNames.has(secret.name)}
              onCheckedChange={() => toggleSecretName(secret.name, secret.legacyId)}
            />
            <label htmlFor={`secret-${secret.name}`} className="flex-1 text-sm font-medium cursor-pointer select-none text-gray-900 dark:text-gray-100">
              {secret.name}
              <span className="ml-2 text-[10px] px-1.5 py-0.5 rounded bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-300">Shared</span>
            </label>
          </div>
        ))}
      </div>
    </div>
  );
};

export default SecretSelectionSection;
