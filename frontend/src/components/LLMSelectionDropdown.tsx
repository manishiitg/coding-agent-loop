import { useState, useEffect, useRef } from 'react';
import { Brain, ChevronDown, Check, RefreshCw, Search, Box, DollarSign, Terminal, KeyRound, AudioLines } from 'lucide-react';
import { Button } from './ui/Button';
import { Card } from './ui/Card';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip';
import type { LLMOption } from '../types/llm';
import {
  getProviderDisplayInfo,
  getProviderIntegrationInfo,
  getProviderIntegrationKind,
  LLM_INTEGRATION_ORDER,
  shouldShowLLMPricing,
  type LLMIntegrationKind,
} from '../utils/llmDisplay';

// Helper to format context window size
const formatContextWindow = (tokens?: number): string => {
  if (!tokens) return '';
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`;
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(0)}k`;
  return `${tokens}`;
};

// Helper to format cost
const formatCost = (cost?: number): string => {
  if (cost === undefined || cost === null) return '';
  return `$${cost.toFixed(2)}`;
};

// Helper to get options summary
const getOptionsSummary = (options?: Record<string, unknown>): string => {
  if (!options || Object.keys(options).length === 0) return '';
  const parts: string[] = [];
  if (options.reasoning_effort) parts.push(`Reasoning: ${options.reasoning_effort}`);
  if (options.thinking_level) parts.push(`Thinking: ${options.thinking_level}`);
  if (options.thinking_budget) parts.push(`Budget: ${options.thinking_budget}`);
  return parts.join(' • ');
};

const SECTION_INFO: Record<NonNullable<LLMOption['section']>, {
  label: string;
  toneClass: string;
  icon: typeof Terminal;
}> = {
  coding_agent: {
    label: 'Coding Agents',
    toneClass: 'text-amber-700 dark:text-amber-300',
    icon: Terminal,
  },
  published_model: {
    label: 'Published Models',
    toneClass: 'text-blue-700 dark:text-blue-300',
    icon: KeyRound,
  },
};

type LLMOptionGroup = {
  key: string;
  label: string;
  toneClass: string;
  icon: typeof Terminal;
  llms: LLMOption[];
};

interface LLMSelectionDropdownProps {
  availableLLMs: LLMOption[];
  selectedLLM: LLMOption | null;
  onLLMSelect: (llm: LLMOption) => void;
  onRefresh?: () => void;
  disabled?: boolean;
  inModal?: boolean; // Add prop to indicate if used inside a modal
  openDirection?: 'up' | 'down'; // Add prop to control dropdown direction
  title?: string; // Custom title for the dropdown modal (defaults to "Select Primary LLM")
  placeholder?: string; // Custom placeholder text when no LLM is selected
}

export default function LLMSelectionDropdown({
  availableLLMs,
  selectedLLM,
  onLLMSelect,
  onRefresh,
  disabled = false,
  inModal = false,
  openDirection = 'down', // Default to downward
  title = 'Select Primary LLM', // Default title
  placeholder
}: LLMSelectionDropdownProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const searchInputRef = useRef<HTMLInputElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Auto-focus search input when dropdown opens
  useEffect(() => {
    if (isOpen && searchInputRef.current) {
      searchInputRef.current.focus();
    }
  }, [isOpen]);

  // Clear search when dropdown closes
  useEffect(() => {
    if (!isOpen) {
      setSearchQuery('');
    }
  }, [isOpen]);

  // Handle clicks outside and keyboard events when in modal
  useEffect(() => {
    if (isOpen && inModal) {
      const handleClickOutside = (event: MouseEvent) => {
        const target = event.target as Element;
        if (!target.closest('[data-llm-dropdown]')) {
          setIsOpen(false);
        }
      };
      const handleKeyDown = (e: KeyboardEvent) => {
        if (e.key === 'Escape') setIsOpen(false);
      };

      document.addEventListener('mousedown', handleClickOutside);
      document.addEventListener('keydown', handleKeyDown);
      return () => {
        document.removeEventListener('mousedown', handleClickOutside);
        document.removeEventListener('keydown', handleKeyDown);
      };
    }
  }, [isOpen, inModal]);

  const handleLLMSelect = (llm: LLMOption) => {
    onLLMSelect(llm);
  };

  const getDisplayText = (truncate: boolean = true) => {
    if (selectedLLM) {
      const label = selectedLLM.label;
      if (truncate) {
        // Truncate to 10 characters if too long
        return label.length > 10 ? label.substring(0, 10) + '…' : label;
      }
      return label;
    }
    return placeholder || 'Select LLM';
  };

  // Filter LLMs based on search query
  const filteredLLMs = searchQuery.trim()
    ? availableLLMs.filter((llm) => {
        const query = searchQuery.toLowerCase();
        return (
          llm.model.toLowerCase().includes(query) ||
          llm.label.toLowerCase().includes(query) ||
          llm.provider.toLowerCase().includes(query) ||
          (llm.description && llm.description.toLowerCase().includes(query))
        );
      })
    : availableLLMs;

  const integrationIcons: Record<LLMIntegrationKind, typeof Terminal> = {
    coding_agent: Terminal,
    api_model: KeyRound,
    audio_provider: AudioLines,
  };

  const usesCustomSections = filteredLLMs.some((llm) => llm.section);
  const groupedLLMs: LLMOptionGroup[] = usesCustomSections
    ? (['coding_agent', 'published_model'] as Array<NonNullable<LLMOption['section']>>).map((section) => ({
        key: section,
        label: SECTION_INFO[section].label,
        toneClass: SECTION_INFO[section].toneClass,
        icon: SECTION_INFO[section].icon,
        llms: filteredLLMs.filter((llm) => llm.section === section),
      })).filter((group) => group.llms.length > 0)
    : LLM_INTEGRATION_ORDER.map((kind) => {
        const firstForKind = filteredLLMs.find((llm) => getProviderIntegrationKind(llm.provider, llm.model) === kind);
        const integrationInfo = getProviderIntegrationInfo(firstForKind?.provider, firstForKind?.model);
        return {
        key: kind,
        label: integrationInfo.label,
        toneClass: integrationInfo.toneClass,
        icon: integrationIcons[kind],
        llms: filteredLLMs.filter((llm) => getProviderIntegrationKind(llm.provider, llm.model) === kind),
        };
      }).filter((group) => group.llms.length > 0);
  const selectedShowPricing = selectedLLM ? shouldShowLLMPricing(selectedLLM.provider, selectedLLM.model) : false;

  return (
    <TooltipProvider>
      <div className="relative" data-llm-dropdown style={inModal && isOpen ? { zIndex: 99999 } : undefined}>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              ref={buttonRef}
              type="button"
              onClick={() => {
                setIsOpen(!isOpen);
              }}
              disabled={disabled || availableLLMs.length === 0}
              className={`group flex items-center h-8 px-2 text-xs font-medium bg-background border border-border hover:bg-secondary text-foreground rounded-md transition-all duration-200 ${
                disabled || availableLLMs.length === 0 ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'
              }`}
              aria-expanded={isOpen}
              aria-haspopup="menu"
              aria-label={title}
            >
              <Brain className="w-3.5 h-3.5 mr-1.5 flex-shrink-0" />
              <span className="inline-block overflow-hidden whitespace-nowrap w-[140px] group-hover:w-[240px] transition-[width] duration-300 text-left">
                {getDisplayText(false)}
              </span>
              <ChevronDown className="w-3.5 h-3.5 ml-1 flex-shrink-0" />
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{availableLLMs.length === 0 ? 'No LLMs available' : title}</p>
          </TooltipContent>
        </Tooltip>

        {isOpen && (
          <>
            {/* Backdrop - only show when not in modal */}
            {!inModal && (
              <div 
                className="fixed inset-0 z-40"
                onClick={() => setIsOpen(false)}
              />
            )}
            
            {/* Dropdown */}
            <div 
              ref={dropdownRef}
              className={`${inModal ? 'fixed' : 'absolute'} left-0 ${inModal ? 'z-[99999]' : 'z-50'} min-w-[300px] ${
                openDirection === 'up' 
                  ? 'bottom-full mb-1' 
                  : 'top-full mt-1'
              }`}
              onClick={(e) => e.stopPropagation()}
              role="menu"
              aria-label="LLM selection menu"
              style={inModal && buttonRef.current ? (() => {
                const rect = buttonRef.current.getBoundingClientRect();
                return {
                  zIndex: 99999,
                  top: openDirection === 'up' ? `${rect.top - 200}px` : `${rect.bottom + 4}px`,
                  left: `${rect.left}px`,
                };
              })() : inModal ? { zIndex: 99999 } : undefined}
            >
              <Card className="p-4 shadow-lg border-border bg-card" style={inModal ? { zIndex: 99999, position: 'relative' } : undefined} onClick={(e) => e.stopPropagation()}>
                <div className="space-y-3">
                  {/* Header */}
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-medium text-foreground">
                      {title}
                    </h3>
                    <div className="flex items-center gap-1">
                      {/* Search Input */}
                      <div className="relative flex items-center">
                        <Search className="absolute left-2 w-3 h-3 text-muted-foreground pointer-events-none" />
                        <input
                          ref={searchInputRef}
                          type="text"
                          placeholder="Search..."
                          value={searchQuery}
                          onChange={(e) => {
                            e.stopPropagation();
                            setSearchQuery(e.target.value);
                          }}
                          onClick={(e) => e.stopPropagation()}
                          className="h-6 w-24 pl-7 pr-2 text-xs bg-background border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary"
                        />
                      </div>
                      {onRefresh && (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              onClick={(e) => {
                                e.stopPropagation();
                                onRefresh();
                              }}
                              className="h-6 w-6 p-0 text-muted-foreground hover:text-foreground"
                            >
                              <RefreshCw className="w-3 h-3" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>Refresh LLM list</p>
                          </TooltipContent>
                        </Tooltip>
                      )}
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={(e) => {
                          e.stopPropagation();
                          setIsOpen(false);
                        }}
                        className="h-6 w-6 p-0 text-muted-foreground hover:text-foreground"
                      >
                        ✕
                      </Button>
                    </div>
                  </div>

                  {/* LLM List - Grouped by Integration */}
                  <div className="max-h-48 overflow-y-auto space-y-2 border-border border rounded-md p-2 bg-background">
                    {filteredLLMs.length > 0 ? (
                      groupedLLMs.map((group) => {
                        const IntegrationIcon = group.icon;
                        const providerGroups = group.llms.reduce((groups, llm) => {
                          if (!groups[llm.provider]) {
                            groups[llm.provider] = [];
                          }
                          groups[llm.provider].push(llm);
                          return groups;
                        }, {} as Record<string, LLMOption[]>);

                        return (
                          <div key={group.key} className="space-y-1">
                            <div className={`flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide px-2 py-1 bg-secondary rounded ${group.toneClass}`}>
                              <IntegrationIcon className="w-3.5 h-3.5" />
                              {group.label}
                            </div>

                            {Object.entries(providerGroups).map(([provider, llms]) => {
                              const providerInfo = getProviderDisplayInfo(provider);
                              return (
                                <div key={provider} className="space-y-1">
                                  <div className="text-[11px] font-medium text-muted-foreground px-2 pt-1">
                                    {providerInfo.name}
                                  </div>
                                  {llms.map((llm, index) => {
                                    const optionsSummary = getOptionsSummary(llm.options);
                                    const showPricing = shouldShowLLMPricing(llm.provider, llm.model);
                                    const hasMetadata = llm.contextWindow || (showPricing && llm.inputCostPer1M !== undefined);

                                    return (
                                      <div
                                        key={`${provider}-${llm.model}-${index}`}
                                        className="flex items-start space-x-2 p-2 rounded-md hover:bg-secondary cursor-pointer ml-2"
                                        onClick={(e) => {
                                          e.stopPropagation();
                                          handleLLMSelect(llm);
                                          setIsOpen(false);
                                        }}
                                        role="menuitem"
                                        tabIndex={0}
                                        onKeyDown={(e) => {
                                          if (e.key === 'Enter' || e.key === ' ') {
                                            e.preventDefault();
                                            handleLLMSelect(llm);
                                            setIsOpen(false);
                                          }
                                        }}
                                        aria-label={`Select ${llm.label}`}
                                      >
                                        <div className="flex-1 min-w-0">
                                          <div className="text-sm font-medium text-foreground truncate">
                                            {llm.label}
                                          </div>
                                          <div className="text-xs text-muted-foreground truncate">
                                            {llm.model}
                                          </div>

                                          {/* Metadata row: context and cost */}
                                          {hasMetadata && (
                                            <div className="flex flex-wrap items-center gap-2 mt-1 text-[10px] text-muted-foreground">
                                              {llm.contextWindow && (
                                                <span className="flex items-center gap-0.5" title="Context window">
                                                  <Box className="w-3 h-3" />
                                                  {formatContextWindow(llm.contextWindow)}
                                                </span>
                                              )}
                                              {showPricing && llm.inputCostPer1M !== undefined && (
                                                <span className="flex items-center gap-0.5" title="Input cost per 1M tokens">
                                                  <DollarSign className="w-3 h-3" />
                                                  {formatCost(llm.inputCostPer1M)}/1M
                                                </span>
                                              )}
                                            </div>
                                          )}

                                          {/* Options row: reasoning, thinking, etc. */}
                                          {optionsSummary && (
                                            <div className="text-[10px] text-primary/70 mt-0.5">
                                              {optionsSummary}
                                            </div>
                                          )}
                                        </div>
                                        {selectedLLM && selectedLLM.provider === llm.provider && selectedLLM.model === llm.model && (
                                          <Check className="w-4 h-4 text-primary flex-shrink-0 mt-0.5" />
                                        )}
                                      </div>
                                    );
                                  })}
                                </div>
                              );
                            })}
                          </div>
                        );
                      })
                    ) : availableLLMs.length > 0 ? (
                      <div className="text-sm text-muted-foreground text-center py-4">
                        No LLMs found matching "{searchQuery}"
                      </div>
                    ) : (
                      <div className="text-sm text-muted-foreground text-center py-4">
                        No LLMs available. Check your configuration.
                      </div>
                    )}
                  </div>

                  {/* Selected LLM Info */}
                  <div className="text-xs text-muted-foreground space-y-1">
                    {selectedLLM ? (
                      <>
                        <div className="font-medium text-foreground">Selected: {selectedLLM.label}</div>
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="px-1.5 py-0.5 bg-secondary rounded text-[10px] capitalize">
                            {selectedLLM.provider}
                          </span>
                          {selectedLLM.contextWindow && (
                            <span className="flex items-center gap-0.5">
                              <Box className="w-3 h-3" />
                              {formatContextWindow(selectedLLM.contextWindow)} ctx
                            </span>
                          )}
                          {selectedShowPricing && selectedLLM.inputCostPer1M !== undefined && (
                            <span>
                              {formatCost(selectedLLM.inputCostPer1M)}/1M in
                            </span>
                          )}
                        </div>
                        {getOptionsSummary(selectedLLM.options) && (
                          <div className="text-[10px] text-primary/70">
                            {getOptionsSummary(selectedLLM.options)}
                          </div>
                        )}
                      </>
                    ) : (
                      'No LLM selected - will use default'
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
