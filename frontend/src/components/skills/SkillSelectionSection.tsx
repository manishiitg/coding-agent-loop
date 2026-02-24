import React, { useCallback, useEffect, useState } from 'react';
import { Checkbox } from '../ui/checkbox';
import { Loader2, Sparkles, Check, RefreshCw } from 'lucide-react';
import { skillsApi } from '../../api/skills';
import type { Skill } from '../../types/skills';

interface SkillSelectionSectionProps {
  selectedSkills: string[]; // Array of skill folder names
  onSkillChange: (skills: string[]) => void;
}

export const SkillSelectionSection: React.FC<SkillSelectionSectionProps> = ({
  selectedSkills,
  onSkillChange,
}) => {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load skills on mount
  useEffect(() => {
    loadSkills();
  }, []);

  const loadSkills = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await skillsApi.listSkills();
      // Deduplicate by file_path to prevent React duplicate key crashes
      const raw = response.skills || [];
      const seen = new Set<string>();
      const unique = raw.filter(s => {
        const key = s.file_path || s.folder_name;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      });
      setSkills(unique);
    } catch (err) {
      console.error('Failed to load skills:', err);
      setError('Failed to load skills');
      setSkills([]);
    } finally {
      setIsLoading(false);
    }
  };

  // Handle skill checkbox toggle
  const handleSkillToggle = useCallback((folderName: string) => {
    const isSelected = selectedSkills.includes(folderName);
    if (isSelected) {
      onSkillChange(selectedSkills.filter(s => s !== folderName));
    } else {
      onSkillChange([...selectedSkills, folderName]);
    }
  }, [selectedSkills, onSkillChange]);

  // Handle select all
  const handleSelectAll = useCallback(() => {
    const allFolderNames = skills.map(s => s.folder_name);
    const allSelected = allFolderNames.every(fn => selectedSkills.includes(fn));

    if (allSelected) {
      // Deselect all
      onSkillChange([]);
    } else {
      // Select all
      onSkillChange(allFolderNames);
    }
  }, [skills, selectedSkills, onSkillChange]);

  const allSelected = skills.length > 0 && skills.every(s => selectedSkills.includes(s.folder_name));

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <label className="block text-sm font-medium text-gray-900 dark:text-gray-100 flex items-center gap-2">
          <Sparkles className="w-4 h-4 text-purple-500" />
          Skills Selection
        </label>
        <button
          type="button"
          onClick={loadSkills}
          disabled={isLoading}
          className="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 flex items-center gap-1"
        >
          <RefreshCw className={`w-3 h-3 ${isLoading ? 'animate-spin' : ''}`} />
          Refresh
        </button>
      </div>

      <div className="text-xs text-gray-500 dark:text-gray-400">
        Select skills to enable for this preset. Skills provide reusable instructions for the agent.
      </div>

      {/* Skills List */}
      <div className="border border-gray-200 dark:border-gray-700 rounded-md max-h-64 overflow-y-auto">
        {isLoading ? (
          <div className="flex items-center justify-center gap-2 text-sm text-gray-500 dark:text-gray-400 py-8">
            <Loader2 className="w-4 h-4 animate-spin" />
            Loading skills...
          </div>
        ) : error ? (
          <div className="text-sm text-red-500 text-center py-8">
            {error}
          </div>
        ) : skills.length === 0 ? (
          <div className="text-sm text-gray-500 text-center py-8">
            No skills available. Import skills from the Skills Manager.
          </div>
        ) : (
          <>
            {/* Select All Header */}
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
                  <>Select all skills</>
                )}
              </button>
              <span className="ml-auto text-xs text-gray-500">
                {selectedSkills.length}/{skills.length} selected
              </span>
            </div>

            {/* Skill Items */}
            {skills
              .sort((a, b) => {
                // Sort selected skills first
                const aSelected = selectedSkills.includes(a.folder_name);
                const bSelected = selectedSkills.includes(b.folder_name);
                if (aSelected && !bSelected) return -1;
                if (!aSelected && bSelected) return 1;
                return a.frontmatter.name.localeCompare(b.frontmatter.name);
              })
              .map((skill) => {
                const isSelected = selectedSkills.includes(skill.folder_name);
                return (
                  <div
                    key={skill.file_path || skill.folder_name}
                    className="flex items-start p-3 hover:bg-gray-100 dark:hover:bg-gray-700 border-b border-gray-200 dark:border-gray-700 last:border-b-0"
                  >
                    <Checkbox
                      id={`preset-skill-${skill.folder_name}`}
                      checked={isSelected}
                      onCheckedChange={() => handleSkillToggle(skill.folder_name)}
                      className="mt-0.5"
                    />
                    <label
                      htmlFor={`preset-skill-${skill.folder_name}`}
                      className="ml-2 text-sm cursor-pointer flex-1"
                    >
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-gray-900 dark:text-gray-100">
                          {skill.frontmatter.name}
                        </span>
                        {isSelected && (
                          <Check className="w-3 h-3 text-green-600 flex-shrink-0" />
                        )}
                      </div>
                      {skill.frontmatter.description && (
                        <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                          {skill.frontmatter.description}
                        </div>
                      )}
                    </label>
                  </div>
                );
              })}
          </>
        )}
      </div>

      {/* Selection Summary */}
      {selectedSkills.length > 0 && (
        <div className="text-xs text-gray-500 dark:text-gray-400">
          Selected: {selectedSkills.length} skill{selectedSkills.length !== 1 ? 's' : ''}
        </div>
      )}
    </div>
  );
};

export default SkillSelectionSection;
