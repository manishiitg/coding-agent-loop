import { useState, useEffect, useCallback } from 'react';
import { KeyRound, Check, Settings, Globe, Square, CheckSquare } from 'lucide-react';
import { Button } from '../ui/Button';
import { Card } from '../ui/Card';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip';
import { useSecretsStore } from '../../stores';
import SecretsManagerModal from './SecretsManagerModal';

interface SecretSelectionDropdownProps {
  selectedSecrets: string[];
  onSecretToggle: (secretId: string) => void;
  onSelectAll: (allSecretIds: string[]) => void;
  onClearAll: () => void;
  disabled?: boolean;
}

export default function SecretSelectionDropdown({
  selectedSecrets,
  onSecretToggle,
  onSelectAll,
  onClearAll,
  disabled = false
}: SecretSelectionDropdownProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [showManager, setShowManager] = useState(false);
  const secrets = useSecretsStore((s) => s.secrets);
  const globalSecrets = useSecretsStore((s) => s.globalSecrets);
  const fetchGlobalSecrets = useSecretsStore((s) => s.fetchGlobalSecrets);
  const selectedGlobalSecrets = useSecretsStore((s) => s.selectedGlobalSecretNames);
  const setSelectedGlobalSecrets = useSecretsStore((s) => s.setSelectedGlobalSecretNames);

  useEffect(() => {
    if (globalSecrets.length === 0) {
      fetchGlobalSecrets();
    }
  }, []);

  // Global secret helpers
  const isGlobalSelected = (name: string) =>
    !selectedGlobalSecrets || selectedGlobalSecrets.includes(name);
  const selectedGlobalCount = !selectedGlobalSecrets
    ? globalSecrets.length
    : selectedGlobalSecrets.length;

  const totalSelected = selectedSecrets.length + selectedGlobalCount;
  const hasSelected = totalSelected > 0;

  const handleGlobalToggle = useCallback((name: string) => {
    const store = useSecretsStore.getState();
    const current = store.selectedGlobalSecretNames;
    const allGlobals = store.globalSecrets;
    if (!current) {
      store.setSelectedGlobalSecretNames(allGlobals.map(g => g.name).filter(n => n !== name));
    } else if (current.includes(name)) {
      store.setSelectedGlobalSecretNames(current.filter(n => n !== name));
    } else {
      const newList = [...current, name];
      store.setSelectedGlobalSecretNames(newList.length === allGlobals.length ? null : newList);
    }
  }, []);

  const handleSelectAll = useCallback(() => {
    onSelectAll(secrets.map(s => s.id));
    useSecretsStore.getState().setSelectedGlobalSecretNames(null);
  }, [secrets, onSelectAll]);

  const handleClearAll = useCallback(() => {
    onClearAll();
    useSecretsStore.getState().setSelectedGlobalSecretNames([]);
  }, [onClearAll]);

  const labelText = hasSelected
    ? `${totalSelected} secret${totalSelected > 1 ? 's' : ''}`
    : 'Secrets';

  return (
    <TooltipProvider>
      <div className="relative">
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={() => setIsOpen(!isOpen)}
              disabled={disabled}
              className={`group flex items-center gap-1 p-1.5 rounded-md border transition-all duration-200 ${
                hasSelected
                  ? 'bg-amber-100 dark:bg-amber-900/40 border-amber-400 dark:border-amber-600 text-amber-600 dark:text-amber-400'
                  : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
              } ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
            >
              <KeyRound className="w-4 h-4 flex-shrink-0" />
              <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[70px] transition-all duration-200">
                {labelText}
              </span>
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{hasSelected ? `${totalSelected} secret${totalSelected > 1 ? 's' : ''} selected` : 'Select secrets to inject into chat'}</p>
          </TooltipContent>
        </Tooltip>

        {isOpen && (
          <>
            <div
              className="fixed inset-0 z-40"
              onClick={() => setIsOpen(false)}
            />

            <div className="absolute bottom-full left-0 mb-1 z-50 w-64">
              <Card className="p-4 shadow-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">
                      Select Secrets
                    </h3>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => setIsOpen(false)}
                      className="h-6 w-6 p-0 text-gray-400 hover:text-gray-600"
                    >
                      ✕
                    </Button>
                  </div>

                  <div className="flex gap-2">
                    <Button type="button" variant="outline" size="sm" onClick={handleSelectAll} className="h-7 px-2 text-xs">
                      All
                    </Button>
                    <Button type="button" variant="outline" size="sm" onClick={handleClearAll} className="h-7 px-2 text-xs">
                      None
                    </Button>
                    <Button type="button" variant="ghost" size="sm" onClick={() => { setIsOpen(false); setShowManager(true); }} className="h-7 px-2 text-xs ml-auto">
                      <Settings className="w-3 h-3 mr-1" />
                      Manage
                    </Button>
                  </div>

                  <div className="max-h-64 overflow-y-auto border border-gray-200 dark:border-gray-600 rounded-md bg-gray-50 dark:bg-gray-900">
                    {/* Global Secrets */}
                    {globalSecrets.length > 0 && (
                      <>
                        {globalSecrets.map((gs) => {
                          const checked = isGlobalSelected(gs.name);
                          return (
                            <div
                              key={`global-${gs.name}`}
                              className="flex items-center gap-2 p-2 hover:bg-gray-100 dark:hover:bg-gray-700 border-b border-gray-200 dark:border-gray-700 cursor-pointer"
                              onClick={() => handleGlobalToggle(gs.name)}
                            >
                              {checked
                                ? <CheckSquare className="w-4 h-4 text-blue-500 dark:text-blue-400 flex-shrink-0" />
                                : <Square className="w-4 h-4 text-gray-400 flex-shrink-0" />
                              }
                              <Globe className="w-3.5 h-3.5 text-blue-500 dark:text-blue-400 flex-shrink-0" />
                              <span className="text-sm font-medium font-mono text-gray-900 dark:text-gray-100 flex-1 min-w-0 truncate">
                                {gs.name}
                              </span>
                              {checked && <Check className="w-3 h-3 text-green-600 flex-shrink-0" />}
                            </div>
                          );
                        })}
                        {secrets.length > 0 && (
                          <div className="border-b border-gray-300 dark:border-gray-600" />
                        )}
                      </>
                    )}

                    {/* User Secrets */}
                    {secrets.length > 0 ? (
                      <div className="p-2 space-y-1">
                        {secrets.map((secret) => {
                          const checked = selectedSecrets.includes(secret.id);
                          return (
                            <div
                              key={secret.id}
                              className="flex items-start space-x-2 group p-1.5 hover:bg-gray-100 dark:hover:bg-gray-700 rounded cursor-pointer"
                              onClick={() => onSecretToggle(secret.id)}
                            >
                              {checked
                                ? <CheckSquare className="w-4 h-4 text-blue-500 dark:text-blue-400 flex-shrink-0 mt-0.5" />
                                : <Square className="w-4 h-4 text-gray-400 flex-shrink-0 mt-0.5" />
                              }
                              <div className="flex-1 min-w-0">
                                <span className="text-sm font-medium text-gray-900 dark:text-gray-100 flex items-center gap-2">
                                  {secret.name}
                                  {checked && <Check className="w-3 h-3 text-green-600 flex-shrink-0" />}
                                </span>
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    ) : globalSecrets.length === 0 ? (
                      <div className="text-sm text-gray-500 text-center py-4 p-2">
                        No secrets stored. Click "Manage" to add secrets.
                      </div>
                    ) : null}
                  </div>

                  <div className="text-xs text-gray-500">
                    {secrets.length === 0 && globalSecrets.length === 0
                      ? 'Add secrets via the manager'
                      : `${totalSelected} selected (${selectedGlobalCount} global + ${selectedSecrets.length} user)`
                    }
                  </div>
                </div>
              </Card>
            </div>
          </>
        )}
      </div>

      {showManager && (
        <SecretsManagerModal onClose={() => setShowManager(false)} />
      )}
    </TooltipProvider>
  );
}
