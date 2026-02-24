import React, { useCallback, useEffect } from 'react';
import { Checkbox } from '../ui/checkbox';
import { KeyRound, Check, Globe } from 'lucide-react';
import { useSecretsStore } from '../../stores';

interface SecretSelectionSectionProps {
  selectedSecrets: string[];
  onSecretChange: (secrets: string[]) => void;
  selectedGlobalSecrets?: string[] | null; // null = all selected
  onGlobalSecretChange?: (names: string[] | null) => void;
}

export const SecretSelectionSection: React.FC<SecretSelectionSectionProps> = ({
  selectedSecrets,
  onSecretChange,
  selectedGlobalSecrets = null,
  onGlobalSecretChange,
}) => {
  const secrets = useSecretsStore((s) => s.secrets);
  const globalSecrets = useSecretsStore((s) => s.globalSecrets);
  const fetchGlobalSecrets = useSecretsStore((s) => s.fetchGlobalSecrets);

  useEffect(() => {
    if (globalSecrets.length === 0) {
      fetchGlobalSecrets();
    }
  }, []);

  const isGlobalSelected = (name: string) =>
    !selectedGlobalSecrets || selectedGlobalSecrets.includes(name);

  const handleGlobalToggle = useCallback((name: string) => {
    if (!onGlobalSecretChange) return;
    if (selectedGlobalSecrets === null) {
      // All selected -> deselect this one
      const newList = globalSecrets.map(g => g.name).filter(n => n !== name);
      onGlobalSecretChange(newList);
    } else if (selectedGlobalSecrets.includes(name)) {
      onGlobalSecretChange(selectedGlobalSecrets.filter(n => n !== name));
    } else {
      const newList = [...selectedGlobalSecrets, name];
      // If all are now selected, set to null
      if (newList.length === globalSecrets.length) {
        onGlobalSecretChange(null);
      } else {
        onGlobalSecretChange(newList);
      }
    }
  }, [selectedGlobalSecrets, globalSecrets, onGlobalSecretChange]);

  const handleSecretToggle = useCallback((secretId: string) => {
    const isSelected = selectedSecrets.includes(secretId);
    if (isSelected) {
      onSecretChange(selectedSecrets.filter(s => s !== secretId));
    } else {
      onSecretChange([...selectedSecrets, secretId]);
    }
  }, [selectedSecrets, onSecretChange]);

  const handleSelectAll = useCallback(() => {
    const allIds = secrets.map(s => s.id);
    const allSelected = allIds.every(id => selectedSecrets.includes(id));

    if (allSelected) {
      onSecretChange([]);
    } else {
      onSecretChange(allIds);
    }
  }, [secrets, selectedSecrets, onSecretChange]);

  const allSelected = secrets.length > 0 && secrets.every(s => selectedSecrets.includes(s.id));

  if (secrets.length === 0 && globalSecrets.length === 0) {
    return null;
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <label className="block text-sm font-medium text-gray-900 dark:text-gray-100 flex items-center gap-2">
          <KeyRound className="w-4 h-4 text-amber-500" />
          Secrets Selection
        </label>
      </div>

      <div className="text-xs text-gray-500 dark:text-gray-400">
        Select secrets to inject into all workflow steps. Secrets are decrypted at runtime.
      </div>

      <div className="border border-gray-200 dark:border-gray-700 rounded-md max-h-64 overflow-y-auto">
        {/* Global Secrets (selectable) */}
        {globalSecrets.length > 0 && (
          <>
            {globalSecrets.map((gs) => {
              const gsId = `global-secret-${gs.name}`;
              return (
                <div
                  key={`global-${gs.name}`}
                  className="flex items-center p-3 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700"
                >
                  <Checkbox
                    id={gsId}
                    checked={isGlobalSelected(gs.name)}
                    onCheckedChange={() => handleGlobalToggle(gs.name)}
                    className="flex-shrink-0"
                  />
                  <label
                    htmlFor={gsId}
                    className="flex-1 flex items-center cursor-pointer"
                  >
                    <Globe className="w-3.5 h-3.5 text-blue-500 dark:text-blue-400 flex-shrink-0 ml-2" />
                    <span className="ml-2 text-sm font-medium font-mono text-gray-900 dark:text-gray-100">
                      {gs.name}
                    </span>
                    {isGlobalSelected(gs.name) && (
                      <Check className="w-3 h-3 text-green-600 flex-shrink-0 ml-2" />
                    )}
                    <span className="ml-auto text-[10px] font-medium px-1.5 py-0.5 rounded bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-400">
                      Global
                    </span>
                  </label>
                </div>
              );
            })}
            {secrets.length > 0 && (
              <div className="border-b-2 border-gray-300 dark:border-gray-600" />
            )}
          </>
        )}

        {/* Select All Header (user secrets only) */}
        {secrets.length > 0 && (
          <div className="flex items-center p-3 bg-gray-50 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700">
            <button
              type="button"
              onClick={handleSelectAll}
              className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 flex items-center gap-1"
            >
              {allSelected ? (
                <>
                  <Check className="w-3 h-3" />
                  Deselect all
                </>
              ) : (
                <>Select all secrets</>
              )}
            </button>
            <span className="ml-auto text-xs text-gray-500">
              {selectedSecrets.length}/{secrets.length} selected
            </span>
          </div>
        )}

        {/* User Secret Items */}
        {secrets
          .sort((a, b) => {
            const aSelected = selectedSecrets.includes(a.id);
            const bSelected = selectedSecrets.includes(b.id);
            if (aSelected && !bSelected) return -1;
            if (!aSelected && bSelected) return 1;
            return a.name.localeCompare(b.name);
          })
          .map((secret) => {
            const isSelected = selectedSecrets.includes(secret.id);
            return (
              <div
                key={secret.id}
                className="flex items-start p-3 hover:bg-gray-100 dark:hover:bg-gray-700 border-b border-gray-200 dark:border-gray-700 last:border-b-0"
              >
                <Checkbox
                  id={`preset-secret-${secret.id}`}
                  checked={isSelected}
                  onCheckedChange={() => handleSecretToggle(secret.id)}
                  className="mt-0.5"
                />
                <label
                  htmlFor={`preset-secret-${secret.id}`}
                  className="ml-2 text-sm cursor-pointer flex-1"
                >
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-gray-900 dark:text-gray-100">
                      {secret.name}
                    </span>
                    {isSelected && (
                      <Check className="w-3 h-3 text-green-600 flex-shrink-0" />
                    )}
                  </div>
                </label>
              </div>
            );
          })}
      </div>

      {selectedSecrets.length > 0 && (
        <div className="text-xs text-gray-500 dark:text-gray-400">
          Selected: {selectedSecrets.length} secret{selectedSecrets.length !== 1 ? 's' : ''}
        </div>
      )}
    </div>
  );
};

export default SecretSelectionSection;
