import { useState, useEffect } from 'react';
import { Sparkles, ChevronDown, Check } from 'lucide-react';
import { Button } from '../ui/Button';
import { Checkbox } from '../ui/checkbox';
import { Card } from '../ui/Card';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip';
import { skillsApi } from '../../api/skills';
import type { Skill } from '../../types/skills';

interface SkillSelectionDropdownProps {
  selectedSkills: string[];
  onSkillToggle: (skillFolderName: string) => void;
  onSelectAll: (allSkillNames: string[]) => void;
  onClearAll: () => void;
  disabled?: boolean;
}

export default function SkillSelectionDropdown({
  selectedSkills,
  onSkillToggle,
  onSelectAll,
  onClearAll,
  disabled = false
}: SkillSelectionDropdownProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  // Load skills when dropdown opens
  useEffect(() => {
    if (isOpen && skills.length === 0) {
      loadSkills();
    }
  }, [isOpen]);

  const loadSkills = async () => {
    setIsLoading(true);
    try {
      const response = await skillsApi.listSkills();
      setSkills(response.skills || []);
    } catch (err) {
      console.error('Failed to load skills:', err);
      setSkills([]);
    } finally {
      setIsLoading(false);
    }
  };

  const getDisplayText = () => {
    if (selectedSkills.length === 0) {
      return "No skills";
    } else if (selectedSkills.length === 1) {
      const skill = skills.find(s => s.folder_name === selectedSkills[0]);
      return skill?.frontmatter.name || selectedSkills[0];
    } else {
      return `${selectedSkills.length} skills`;
    }
  };

  const isAllSelected = skills.length > 0 && selectedSkills.length === skills.length;
  const isNoneSelected = selectedSkills.length === 0;

  return (
    <TooltipProvider>
      <div className="relative">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsOpen(!isOpen)}
              disabled={disabled}
              className="h-8 px-2 text-xs font-medium bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-700"
            >
              <Sparkles className="w-3 h-3 mr-1 text-purple-500" />
              {getDisplayText()}
              <ChevronDown className="w-3 h-3 ml-1" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Select skills to include in chat</p>
          </TooltipContent>
        </Tooltip>

        {isOpen && (
          <>
            {/* Backdrop */}
            <div
              className="fixed inset-0 z-40"
              onClick={() => setIsOpen(false)}
            />

            {/* Dropdown */}
            <div className="absolute bottom-full left-0 mb-1 z-50 w-64">
              <Card className="p-4 shadow-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
                <div className="space-y-3">
                  {/* Header */}
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">
                      Select Skills
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

                  {/* Quick Actions */}
                  <div className="flex gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => onSelectAll(skills.map(s => s.folder_name))}
                      disabled={isAllSelected || skills.length === 0}
                      className="h-7 px-2 text-xs"
                    >
                      All
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={onClearAll}
                      disabled={isNoneSelected}
                      className="h-7 px-2 text-xs"
                    >
                      None
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={loadSkills}
                      disabled={isLoading}
                      className="h-7 px-2 text-xs ml-auto"
                    >
                      {isLoading ? 'Loading...' : 'Refresh'}
                    </Button>
                  </div>

                  {/* Skills List */}
                  <div className="max-h-64 overflow-y-auto space-y-1 border border-gray-200 dark:border-gray-600 rounded-md p-2 bg-gray-50 dark:bg-gray-900">
                    {isLoading ? (
                      <div className="text-sm text-gray-500 text-center py-4">
                        Loading skills...
                      </div>
                    ) : skills.length > 0 ? (
                      skills.map((skill) => (
                        <div key={skill.folder_name} className="flex items-start space-x-2 group p-1.5 hover:bg-gray-100 dark:hover:bg-gray-700 rounded cursor-pointer">
                          <Checkbox
                            id={`skill-${skill.folder_name}`}
                            checked={selectedSkills.includes(skill.folder_name)}
                            onCheckedChange={() => onSkillToggle(skill.folder_name)}
                            className="h-4 w-4 mt-0.5"
                          />
                          <div className="flex-1 min-w-0">
                            <label
                              htmlFor={`skill-${skill.folder_name}`}
                              className="text-sm font-medium cursor-pointer text-gray-900 dark:text-gray-100 flex items-center gap-2"
                            >
                              {skill.frontmatter.name}
                              {selectedSkills.includes(skill.folder_name) && (
                                <Check className="w-3 h-3 text-green-600 flex-shrink-0" />
                              )}
                            </label>
                            <p className="text-xs text-gray-500 dark:text-gray-400 truncate">
                              {skill.frontmatter.description.length > 40
                                ? skill.frontmatter.description.slice(0, 40) + '...'
                                : skill.frontmatter.description}
                            </p>
                          </div>
                        </div>
                      ))
                    ) : (
                      <div className="text-sm text-gray-500 text-center py-4">
                        No skills available. Import skills from the Skills Manager.
                      </div>
                    )}
                  </div>

                  {/* Instructions */}
                  <div className="text-xs text-gray-500">
                    {skills.length === 0
                      ? 'Import skills from Skills Manager'
                      : `${selectedSkills.length}/${skills.length} selected`
                    }
                  </div>
                </div>
              </Card>
            </div>
          </>
        )}
      </div>
    </TooltipProvider>
  );
}
