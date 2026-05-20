import React, { useEffect, useMemo, useState } from 'react';
import { Checkbox } from '../ui/checkbox';
import { KeyRound, Globe, Plus, Trash2 } from 'lucide-react';
import { useSecretsStore } from '../../stores';
import { secretsApi } from '../../api/secrets';

interface SecretSelectionSectionProps {
  selectedSecrets: string[];
  onSecretChange: (secrets: string[]) => void;
  selectedGlobalSecrets?: string[] | null; // null = all selected, [] = none selected
  onGlobalSecretChange?: (names: string[] | null) => void;
  workflowPath?: string;
}

const isValidName = (name: string) => /^[A-Za-z_][A-Za-z0-9_]*$/.test(name);

export const SecretSelectionSection: React.FC<SecretSelectionSectionProps> = ({
  selectedSecrets,
  onSecretChange,
  selectedGlobalSecrets = [],
  onGlobalSecretChange,
  workflowPath,
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
  const [workflowSecretName, setWorkflowSecretName] = useState('');
  const [workflowSecretValue, setWorkflowSecretValue] = useState('');
  const [workflowSecretError, setWorkflowSecretError] = useState<string | null>(null);
  const [savingWorkflowSecret, setSavingWorkflowSecret] = useState(false);

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
    const isSelected = selectedGlobalSecrets === null || selectedGlobalSecrets.includes(name);
    if (isSelected) {
      const remaining = (selectedGlobalSecrets ?? globalSecrets.map(g => g.name)).filter(n => n !== name);
      onGlobalSecretChange(remaining);
    } else {
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
      setWorkflowSecretError(err instanceof Error ? err.message : 'Failed to save workflow secret');
    } finally {
      setSavingWorkflowSecret(false);
    }
  };

  const handleDeleteWorkflowSecret = async (name: string) => {
    if (!normalizedWorkflowPath) return;
    if (!confirm(`Delete workflow secret "${name}"?`)) return;
    await removeWorkflowSecret(normalizedWorkflowPath, name);
    onSecretChange(selectedSecrets.filter(s => s !== name));
  };

  if (sharedSecrets.length === 0 && globalSecrets.length === 0 && workflowSecrets.length === 0 && !normalizedWorkflowPath) return null;

  const sortedWorkflowSecrets = [...workflowSecrets].sort((a, b) => a.name.localeCompare(b.name));

  return (
    <div className="space-y-2">
      <label className="block text-sm font-medium text-gray-900 dark:text-gray-100 flex items-center gap-2">
        <KeyRound className="w-4 h-4 text-amber-500" />
        Secrets
      </label>

      {normalizedWorkflowPath && (
        <div className="space-y-2 rounded-md border border-amber-200 bg-amber-50 p-3 dark:border-amber-900/60 dark:bg-amber-950/20">
          <div className="flex items-center justify-between gap-2">
            <div className="min-w-0">
              <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Workflow Secrets</div>
              <div className="truncate text-xs text-gray-500 dark:text-gray-400">{normalizedWorkflowPath}</div>
            </div>
            <span className="shrink-0 rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/60 dark:text-amber-300">
              Private
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

      <div className="border border-gray-200 dark:border-gray-700 rounded-md max-h-64 overflow-y-auto bg-white dark:bg-gray-800">
        {sortedWorkflowSecrets.map((secret) => (
          <div key={`workflow-${secret.name}`} className="flex items-center gap-2 p-3 border-b border-gray-200 dark:border-gray-700 last:border-b-0 hover:bg-gray-100 dark:hover:bg-gray-700">
            <Checkbox
              id={`workflow-secret-${secret.name}`}
              checked={selectedSecretNames.has(secret.name)}
              onCheckedChange={() => toggleSecretName(secret.name)}
            />
            <label htmlFor={`workflow-secret-${secret.name}`} className="flex-1 flex min-w-0 items-center gap-2 text-sm cursor-pointer select-none text-gray-900 dark:text-gray-100">
              <span className="min-w-0 truncate font-mono">{secret.name}</span>
              <span className="ml-auto shrink-0 text-[10px] px-1.5 py-0.5 rounded bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300">Workflow</span>
            </label>
            <button
              type="button"
              onClick={() => handleDeleteWorkflowSecret(secret.name)}
              className="shrink-0 p-1 text-gray-400 transition-colors hover:text-red-600 dark:hover:text-red-400"
              title="Delete workflow secret"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </div>
        ))}

        {globalSecrets.map((gs) => (
          <div key={`global-${gs.name}`} className="flex items-center gap-2 p-3 border-b border-gray-200 dark:border-gray-700 last:border-b-0 hover:bg-gray-100 dark:hover:bg-gray-700">
            <Checkbox
              id={`global-secret-${gs.name}`}
              checked={selectedGlobalSecrets === null || selectedGlobalSecrets.includes(gs.name)}
              onCheckedChange={() => toggleGlobal(gs.name)}
              disabled={!onGlobalSecretChange}
            />
            <label htmlFor={`global-secret-${gs.name}`} className="flex-1 flex items-center gap-2 text-sm cursor-pointer select-none">
              <Globe className="w-3.5 h-3.5 text-blue-500 flex-shrink-0" />
              <span className="font-mono">{gs.name}</span>
              <span className="ml-auto text-[10px] px-1.5 py-0.5 rounded bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-400">Global</span>
            </label>
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
