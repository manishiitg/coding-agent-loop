import { useState, useEffect } from 'react';
import { Bot, Check, HelpCircle, Search } from 'lucide-react';
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
  onImportClick?: () => void;
}

export default function SubAgentSelectionDropdown({
  selectedSubAgents,
  onSubAgentToggle,
  onSelectAll,
  onClearAll,
  disabled = false,
  onImportClick
}: SubAgentSelectionDropdownProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [subagents, setSubAgents] = useState<SubAgent[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [showHelp, setShowHelp] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');

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

  const filteredSubAgents = searchQuery.trim()
    ? subagents.filter(s =>
        s.frontmatter.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        s.frontmatter.description.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : subagents;

  const isAllSelected = subagents.length > 0 && selectedSubAgents.length === subagents.length;
  const isNoneSelected = selectedSubAgents.length === 0;

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
                selectedSubAgents.length > 0
                  ? 'bg-blue-100 dark:bg-blue-900/40 border-blue-400 dark:border-blue-600 text-blue-600 dark:text-blue-400'
                  : 'bg-gray-100 dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-400 dark:text-gray-500'
              } ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:pr-2'}`}
            >
              <Bot className="w-4 h-4 flex-shrink-0" />
              <span className="text-xs font-medium max-w-0 overflow-hidden whitespace-nowrap group-hover:max-w-[90px] transition-all duration-200">
                {getDisplayText()}
              </span>
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{selectedSubAgents.length > 0 ? `${selectedSubAgents.length} sub-agent${selectedSubAgents.length !== 1 ? 's' : ''} selected` : 'Select sub-agent templates for delegation'}</p>
          </TooltipContent>
        </Tooltip>

        {isOpen && (
          <>
            {/* Backdrop */}
            <div
              className="fixed inset-0 z-40"
              onClick={() => { setIsOpen(false); setSearchQuery(''); }}
            />

            {/* Dropdown */}
            <div className="absolute bottom-full left-0 mb-1 z-50 w-72">
              <Card className="p-4 shadow-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
                <div className="space-y-3">
                  {/* Header */}
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1.5">
                      <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">
                        Sub-Agent Templates
                      </h3>
                      <button
                        type="button"
                        onClick={() => setShowHelp(!showHelp)}
                        className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
                        title="What are sub-agents?"
                      >
                        <HelpCircle className="w-3.5 h-3.5" />
                      </button>
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => { setIsOpen(false); setSearchQuery(''); }}
                      className="h-6 w-6 p-0 text-gray-400 hover:text-gray-600"
                    >
                      ✕
                    </Button>
                  </div>

                  {/* Help description */}
                  {showHelp && (
                    <div className="text-xs text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-gray-900 rounded-md p-2 border border-gray-200 dark:border-gray-600">
                      <p className="font-medium text-gray-700 dark:text-gray-300 mb-1">Sub-agents are reusable profiles for delegated agents.</p>
                      <p>When selected, the agent can spawn a separate specialist agent with pre-configured instructions, reasoning level, tools, and skills. Think of them as "specialist roles you can hire" for specific tasks.</p>
                    </div>
                  )}

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

                  {/* Search */}
                  {!isLoading && subagents.length > 0 && (
                    <div className="relative">
                      <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400 pointer-events-none" />
                      <input
                        type="text"
                        placeholder="Search sub-agents..."
                        value={searchQuery}
                        onChange={e => setSearchQuery(e.target.value)}
                        className="w-full pl-7 pr-2 py-1.5 text-xs rounded-md border border-gray-200 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 focus:outline-none focus:ring-1 focus:ring-blue-500"
                      />
                    </div>
                  )}

                  {/* Templates List */}
                  <div className="max-h-64 overflow-y-auto space-y-1 border border-gray-200 dark:border-gray-600 rounded-md p-2 bg-gray-50 dark:bg-gray-900">
                    {isLoading ? (
                      <div className="text-sm text-gray-500 text-center py-4">
                        Loading templates...
                      </div>
                    ) : filteredSubAgents.length > 0 ? (
                      filteredSubAgents.map((sa) => (
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
                            </div>
                          </div>
                        </div>
                      ))
                    ) : subagents.length > 0 ? (
                      <div className="text-sm text-gray-500 text-center py-4">
                        No sub-agents match "{searchQuery}"
                      </div>
                    ) : (
                      <div className="text-sm text-gray-500 text-center py-4">
                        No sub-agent templates available. Create one via /build-subagent.
                      </div>
                    )}
                  </div>

                  {/* Instructions */}
                  <div className="flex items-center justify-between text-xs">
                    <span className="text-gray-500">
                      {subagents.length === 0
                        ? 'Use /build-subagent or import'
                        : `${selectedSubAgents.length}/${subagents.length} selected`
                      }
                    </span>
                    {onImportClick && (
                      <button
                        type="button"
                        onClick={() => {
                          setIsOpen(false);
                          onImportClick();
                        }}
                        className="text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 hover:underline font-medium"
                      >
                        Import Sub-Agent
                      </button>
                    )}
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
