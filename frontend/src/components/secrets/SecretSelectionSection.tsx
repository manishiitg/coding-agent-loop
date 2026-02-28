import React from 'react';
import { Checkbox } from '../ui/checkbox';
import { KeyRound, Globe } from 'lucide-react';
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

  const toggle = (id: string) => {
    if (selectedSecrets.includes(id)) {
      onSecretChange(selectedSecrets.filter(s => s !== id));
    } else {
      onSecretChange([...selectedSecrets, id]);
    }
  };

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

  if (secrets.length === 0 && globalSecrets.length === 0) return null;

  const sorted = [...secrets].sort((a, b) => a.name.localeCompare(b.name));

  return (
    <div className="space-y-2">
      <label className="block text-sm font-medium text-gray-900 dark:text-gray-100 flex items-center gap-2">
        <KeyRound className="w-4 h-4 text-amber-500" />
        Secrets
      </label>

      <div className="border border-gray-200 dark:border-gray-700 rounded-md max-h-64 overflow-y-auto">
        {globalSecrets.map((gs) => (
          <div key={`global-${gs.name}`} className="flex items-center gap-2 p-3 border-b border-gray-200 dark:border-gray-700 last:border-b-0 hover:bg-gray-50 dark:hover:bg-gray-700">
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

        {sorted.map((secret) => (
          <div key={secret.id} className="flex items-center gap-2 p-3 border-b border-gray-200 dark:border-gray-700 last:border-b-0 hover:bg-gray-50 dark:hover:bg-gray-700">
            <Checkbox
              id={`secret-${secret.id}`}
              checked={selectedSecrets.includes(secret.id)}
              onCheckedChange={() => toggle(secret.id)}
            />
            <label htmlFor={`secret-${secret.id}`} className="flex-1 text-sm font-medium cursor-pointer select-none">
              {secret.name}
            </label>
          </div>
        ))}
      </div>
    </div>
  );
};

export default SecretSelectionSection;
