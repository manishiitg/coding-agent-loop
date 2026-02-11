import { useState, useEffect } from 'react';
import { Bot, ChevronDown, Check } from 'lucide-react';
import { Button } from '../ui/Button';
import { Checkbox } from '../ui/checkbox';
import { Card } from '../ui/Card';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip';
import { subagentsApi } from '../../api/subagents';
import type { SubAgent } from '../../types/subagents';

interface SubAgentSelectionDropdownProps {
  selectedSubAgents: string[];
  onSubAgentToggle: (folderName: string) => void;
  onSelectAll: (allNames: string[]) => void;
  onClearAll: () => void;
  disabled?: boolean;
}

export default function SubAgentSelectionDropdown({
  selectedSubAgents,
  onSubAgentToggle,
  onSelectAll,
  onClearAll,
  disabled = false
}: SubAgentSelectionDropdownProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [subagents, setSubAgents] = useState<SubAgent[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  // Load templates when dropdown opens
  useEffect(() => {
    if (isOpen && subagents.length === 0) {
      loadSubAgents();
    }
  }, [isOpen, subagents.length]);

  const loadSubAgents = async () => {
    setIsLoading(true);
    try {
      const response = await subagentsApi.listSubAgents();
      setSubAgents(response.subagents || []);
    } catch (err) {
      console.error('Failed to load sub-agent templates:', err);
      setSubAgents([]);
    } finally {
      setIsLoading(false);
    }
  };

  const getDisplayText = () => {
    if (selectedSubAgents.length === 0) {
      return "No sub-agents";
    } else if (selectedSubAgents.length === 1) {
      const sa = subagents.find(s => s.folder_name === selectedSubAgents[0]);
      return sa?.frontmatter.name || selectedSubAgents[0];
    } else {
      return `${selectedSubAgents.length} sub-agents`;
    }
  };

  const isAllSelected = subagents.length > 0 && selectedSubAgents.length === subagents.length;
  const isNoneSelected = selectedSubAgents.length === 0;

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
              <Bot className="w-3 h-3 mr-1 text-blue-500" />
              {getDisplayText()}
              <ChevronDown className="w-3 h-3 ml-1" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Select sub-agent templates for delegation</p>
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
            <div className="absolute bottom-full left-0 mb-1 z-50 w-72">
              <Card className="p-4 shadow-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
                <div className="space-y-3">
                  {/* Header */}
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">
                      Sub-Agent Templates
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
                      onClick={() => onSelectAll(subagents.map(s => s.folder_name))}
                      disabled={isAllSelected || subagents.length === 0}
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
                      onClick={loadSubAgents}
                      disabled={isLoading}
                      className="h-7 px-2 text-xs ml-auto"
                    >
                      {isLoading ? 'Loading...' : 'Refresh'}
                    </Button>
                  </div>

                  {/* Templates List */}
                  <div className="max-h-64 overflow-y-auto space-y-1 border border-gray-200 dark:border-gray-600 rounded-md p-2 bg-gray-50 dark:bg-gray-900">
                    {isLoading ? (
                      <div className="text-sm text-gray-500 text-center py-4">
                        Loading templates...
                      </div>
                    ) : subagents.length > 0 ? (
                      subagents.map((sa) => (
                        <div key={sa.folder_name} className="flex items-start space-x-2 group p-1.5 hover:bg-gray-100 dark:hover:bg-gray-700 rounded cursor-pointer">
                          <Checkbox
                            id={`subagent-${sa.folder_name}`}
                            checked={selectedSubAgents.includes(sa.folder_name)}
                            onCheckedChange={() => onSubAgentToggle(sa.folder_name)}
                            className="h-4 w-4 mt-0.5"
                          />
                          <div className="flex-1 min-w-0">
                            <label
                              htmlFor={`subagent-${sa.folder_name}`}
                              className="text-sm font-medium cursor-pointer text-gray-900 dark:text-gray-100 flex items-center gap-2"
                            >
                              {sa.frontmatter.name}
                              {selectedSubAgents.includes(sa.folder_name) && (
                                <Check className="w-3 h-3 text-green-600 flex-shrink-0" />
                              )}
                            </label>
                            <p className="text-xs text-gray-500 dark:text-gray-400 truncate">
                              {sa.frontmatter.description.length > 50
                                ? sa.frontmatter.description.slice(0, 50) + '...'
                                : sa.frontmatter.description}
                            </p>
                            {/* Badges */}
                            <div className="flex gap-1 mt-0.5">
                              {sa.frontmatter.default_reasoning_level && (
                                <span className="text-[10px] px-1 bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 rounded">
                                  {sa.frontmatter.default_reasoning_level}
                                </span>
                              )}
                              {sa.frontmatter.default_tool_mode && (
                                <span className="text-[10px] px-1 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded">
                                  {sa.frontmatter.default_tool_mode}
                                </span>
                              )}
                            </div>
                          </div>
                        </div>
                      ))
                    ) : (
                      <div className="text-sm text-gray-500 text-center py-4">
                        No sub-agent templates available. Create one via /build-subagent.
                      </div>
                    )}
                  </div>

                  {/* Instructions */}
                  <div className="text-xs text-gray-500">
                    {subagents.length === 0
                      ? 'Use /build-subagent to create templates'
                      : `${selectedSubAgents.length}/${subagents.length} selected`
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
