import { useState, useEffect } from 'react';
import { KeyRound, Plus, Trash2, Eye, EyeOff, Pencil, X, Globe } from 'lucide-react';
import { useSecretsStore } from '../../stores';
import { secretsApi } from '../../api/secrets';
import type { StoredSecret } from '../../stores';

interface SecretsManagerModalProps {
  onClose: () => void;
}

// Validate secret name: alphanumeric + underscore only
const isValidName = (name: string) => /^[A-Za-z_][A-Za-z0-9_]*$/.test(name);

export default function SecretsManagerModal({ onClose }: SecretsManagerModalProps) {
  const { secrets, addSecret, updateSecret, removeSecret, globalSecrets, fetchGlobalSecrets } = useSecretsStore();

  useEffect(() => {
    if (globalSecrets.length === 0) {
      fetchGlobalSecrets();
    }
  }, []);

  const [newName, setNewName] = useState('');
  const [newValue, setNewValue] = useState('');
  const [isAdding, setIsAdding] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);

  // Edit state
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState('');
  const [editValue, setEditValue] = useState('');
  const [editLoading, setEditLoading] = useState(false);
  const [editError, setEditError] = useState<string | null>(null);

  // Visibility state
  const [visibleIds, setVisibleIds] = useState<Set<string>>(new Set());
  const [decryptedValues, setDecryptedValues] = useState<Record<string, string>>({});
  const [decryptingIds, setDecryptingIds] = useState<Set<string>>(new Set());

  const handleAdd = async () => {
    setAddError(null);
    const trimmedName = newName.trim().toUpperCase();

    if (!trimmedName) {
      setAddError('Name is required');
      return;
    }
    if (!isValidName(trimmedName)) {
      setAddError('Name must start with a letter or underscore and contain only letters, numbers, and underscores');
      return;
    }
    if (secrets.some(s => s.name === trimmedName)) {
      setAddError(`A secret named "${trimmedName}" already exists`);
      return;
    }
    if (!newValue) {
      setAddError('Value is required');
      return;
    }

    setIsAdding(true);
    try {
      const { encrypted } = await secretsApi.encrypt(newValue);
      addSecret({ name: trimmedName, encryptedValue: encrypted });
      setNewName('');
      setNewValue('');
    } catch (err) {
      setAddError(err instanceof Error ? err.message : 'Failed to encrypt secret');
    } finally {
      setIsAdding(false);
    }
  };

  const handleStartEdit = async (secret: StoredSecret) => {
    setEditingId(secret.id);
    setEditName(secret.name);
    setEditValue('');
    setEditError(null);
    setEditLoading(true);
    try {
      const { value } = await secretsApi.decrypt(secret.encryptedValue);
      setEditValue(value);
    } catch (err) {
      setEditError('Failed to decrypt secret value');
    } finally {
      setEditLoading(false);
    }
  };

  const handleSaveEdit = async () => {
    if (!editingId) return;
    setEditError(null);

    const trimmedName = editName.trim().toUpperCase();
    if (!trimmedName) {
      setEditError('Name is required');
      return;
    }
    if (!isValidName(trimmedName)) {
      setEditError('Invalid name format');
      return;
    }
    if (secrets.some(s => s.name === trimmedName && s.id !== editingId)) {
      setEditError(`A secret named "${trimmedName}" already exists`);
      return;
    }
    if (!editValue) {
      setEditError('Value is required');
      return;
    }

    setEditLoading(true);
    try {
      const { encrypted } = await secretsApi.encrypt(editValue);
      updateSecret(editingId, { name: trimmedName, encryptedValue: encrypted });
      setEditingId(null);
      // Clear cached decrypted value
      setDecryptedValues(prev => {
        const next = { ...prev };
        delete next[editingId];
        return next;
      });
      setVisibleIds(prev => {
        const next = new Set(prev);
        next.delete(editingId);
        return next;
      });
    } catch (err) {
      setEditError(err instanceof Error ? err.message : 'Failed to save secret');
    } finally {
      setEditLoading(false);
    }
  };

  const handleToggleVisibility = async (secret: StoredSecret) => {
    if (visibleIds.has(secret.id)) {
      setVisibleIds(prev => {
        const next = new Set(prev);
        next.delete(secret.id);
        return next;
      });
      return;
    }

    // Decrypt on demand
    if (!decryptedValues[secret.id]) {
      setDecryptingIds(prev => new Set(prev).add(secret.id));
      try {
        const { value } = await secretsApi.decrypt(secret.encryptedValue);
        setDecryptedValues(prev => ({ ...prev, [secret.id]: value }));
      } catch {
        return;
      } finally {
        setDecryptingIds(prev => {
          const next = new Set(prev);
          next.delete(secret.id);
          return next;
        });
      }
    }

    setVisibleIds(prev => new Set(prev).add(secret.id));
  };

  const handleDelete = (id: string) => {
    if (!confirm('Are you sure you want to delete this secret?')) return;
    removeSecret(id);
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-6 shadow-xl w-full max-w-lg max-h-[80vh] overflow-hidden flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <KeyRound className="w-5 h-5 text-amber-500" />
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              Secrets Manager
            </h3>
          </div>
          <button
            onClick={onClose}
            className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Global Secrets (read-only) */}
        {globalSecrets.length > 0 && (
          <div className="border border-gray-200 dark:border-gray-600 rounded-md p-3 mb-4 bg-gray-50 dark:bg-gray-900">
            <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2 flex items-center gap-1.5">
              <Globe className="w-3.5 h-3.5 text-blue-500 dark:text-blue-400" />
              Global Secrets
            </h4>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
              Defined via environment variables. Always included in every query.
            </p>
            <div className="space-y-1.5">
              {globalSecrets.map((gs) => (
                <div
                  key={gs.name}
                  className="flex items-center justify-between px-2 py-1.5 bg-white dark:bg-gray-800 rounded border border-gray-200 dark:border-gray-700"
                >
                  <span className="text-sm font-mono font-medium text-gray-900 dark:text-gray-100">
                    {gs.name}
                  </span>
                  <span className="text-xs text-gray-400 font-mono">••••••••</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Add New Secret */}
        <div className="border border-gray-200 dark:border-gray-600 rounded-md p-3 mb-4 bg-gray-50 dark:bg-gray-900">
          <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">Add Secret</h4>
          <div className="space-y-2">
            <input
              type="text"
              placeholder="SECRET_NAME"
              value={newName}
              onChange={(e) => setNewName(e.target.value.toUpperCase())}
              className="w-full px-3 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-amber-500"
            />
            <textarea
              placeholder="Secret value (supports multi-line)"
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
              rows={3}
              className="w-full px-3 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-amber-500 resize-y"
            />
            {addError && (
              <p className="text-xs text-red-500">{addError}</p>
            )}
            <button
              onClick={handleAdd}
              disabled={isAdding}
              className="w-full px-3 py-1.5 text-sm font-medium text-white bg-amber-600 hover:bg-amber-700 disabled:opacity-50 rounded-md transition-colors flex items-center justify-center gap-2"
            >
              <Plus className="w-4 h-4" />
              {isAdding ? 'Encrypting...' : 'Add Secret'}
            </button>
          </div>
        </div>

        {/* Secrets List */}
        <div className="flex-1 overflow-y-auto space-y-2">
          {secrets.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-8 text-gray-500 dark:text-gray-400">
              <KeyRound className="w-10 h-10 mb-3 opacity-50" />
              <p className="text-sm font-medium">No secrets stored</p>
              <p className="text-xs mt-1">Add secrets above to get started</p>
            </div>
          ) : (
            secrets.map((secret) => (
              <div
                key={secret.id}
                className="border border-gray-200 dark:border-gray-600 rounded-md p-3 bg-white dark:bg-gray-800"
              >
                {editingId === secret.id ? (
                  /* Edit Mode */
                  <div className="space-y-2">
                    <input
                      type="text"
                      value={editName}
                      onChange={(e) => setEditName(e.target.value.toUpperCase())}
                      className="w-full px-2 py-1 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-amber-500"
                    />
                    <textarea
                      value={editValue}
                      onChange={(e) => setEditValue(e.target.value)}
                      disabled={editLoading}
                      placeholder={editLoading ? 'Decrypting...' : 'Secret value'}
                      rows={3}
                      className="w-full px-2 py-1 text-sm border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-amber-500 resize-y"
                    />
                    {editError && <p className="text-xs text-red-500">{editError}</p>}
                    <div className="flex gap-2">
                      <button
                        onClick={handleSaveEdit}
                        disabled={editLoading}
                        className="px-3 py-1 text-xs font-medium text-white bg-amber-600 hover:bg-amber-700 disabled:opacity-50 rounded transition-colors"
                      >
                        {editLoading ? 'Saving...' : 'Save'}
                      </button>
                      <button
                        onClick={() => setEditingId(null)}
                        className="px-3 py-1 text-xs font-medium text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 transition-colors"
                      >
                        Cancel
                      </button>
                    </div>
                  </div>
                ) : (
                  /* Display Mode */
                  <div className="flex items-center justify-between">
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-mono font-medium text-gray-900 dark:text-gray-100">
                        {secret.name}
                      </div>
                      <div className="text-xs text-gray-500 dark:text-gray-400 font-mono mt-0.5">
                        {visibleIds.has(secret.id) && decryptedValues[secret.id]
                          ? decryptedValues[secret.id]
                          : '••••••••'
                        }
                      </div>
                    </div>
                    <div className="flex items-center gap-1 ml-2">
                      <button
                        onClick={() => handleToggleVisibility(secret)}
                        disabled={decryptingIds.has(secret.id)}
                        className="p-1.5 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
                        title={visibleIds.has(secret.id) ? 'Hide value' : 'Show value'}
                      >
                        {decryptingIds.has(secret.id) ? (
                          <span className="w-4 h-4 block animate-pulse">...</span>
                        ) : visibleIds.has(secret.id) ? (
                          <EyeOff className="w-4 h-4" />
                        ) : (
                          <Eye className="w-4 h-4" />
                        )}
                      </button>
                      <button
                        onClick={() => handleStartEdit(secret)}
                        className="p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 transition-colors"
                        title="Edit secret"
                      >
                        <Pencil className="w-4 h-4" />
                      </button>
                      <button
                        onClick={() => handleDelete(secret.id)}
                        className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400 transition-colors"
                        title="Delete secret"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                )}
              </div>
            ))
          )}
        </div>

        {/* Footer info */}
        <div className="mt-3 pt-3 border-t border-gray-200 dark:border-gray-700">
          <p className="text-xs text-gray-500 dark:text-gray-400">
            Secrets are encrypted server-side and stored locally. Selected secrets are decrypted and injected into your message before sending.
          </p>
        </div>
      </div>
    </div>
  );
}
