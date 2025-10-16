import { useState, useEffect } from 'react';
import { Brain, ChevronDown, Check, RefreshCw } from 'lucide-react';
import { Button } from './ui/Button';
import { Card } from './ui/Card';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip';
import type { LLMOption } from '../utils/llmConfig';

interface LLMSelectionDropdownProps {
  availableLLMs: LLMOption[];
  selectedLLM: LLMOption | null;
  onLLMSelect: (llm: LLMOption) => void;
  onRefresh?: () => void;
  disabled?: boolean;
  inModal?: boolean; // Add prop to indicate if used inside a modal
  openDirection?: 'up' | 'down'; // Add prop to control dropdown direction
}

export default function LLMSelectionDropdown({
  availableLLMs,
  selectedLLM,
  onLLMSelect,
  onRefresh,
  disabled = false,
  inModal = false,
  openDirection = 'down' // Default to downward
}: LLMSelectionDropdownProps) {
  const [isOpen, setIsOpen] = useState(false);

  // Handle clicks outside when in modal
  useEffect(() => {
    if (isOpen && inModal) {
      const handleClickOutside = (event: MouseEvent) => {
        const target = event.target as Element;
        if (!target.closest('[data-llm-dropdown]')) {
          setIsOpen(false);
        }
      };

      document.addEventListener('mousedown', handleClickOutside);
      return () => {
        document.removeEventListener('mousedown', handleClickOutside);
      };
    }
  }, [isOpen, inModal]);

  const handleLLMSelect = (llm: LLMOption) => {
    onLLMSelect(llm);
  };

  const getDisplayText = () => {
    if (selectedLLM) {
      return selectedLLM.label;
    }
    return 'Select LLM';
  };

  return (
    <TooltipProvider>
      <div className="relative" data-llm-dropdown>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="outline"
              size="sm"
                  onClick={() => {
                    setIsOpen(!isOpen);
                  }}
              disabled={disabled || availableLLMs.length === 0}
              className="h-8 px-2 text-xs font-medium bg-background border-border hover:bg-secondary text-foreground"
            >
              <Brain className="w-3 h-3 mr-1" />
              {getDisplayText()}
              <ChevronDown className="w-3 h-3 ml-1" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{availableLLMs.length === 0 ? 'No LLMs available' : 'Select primary LLM'}</p>
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
              className={`absolute left-0 ${inModal ? 'z-[9999]' : 'z-50'} min-w-[300px] ${
                openDirection === 'up' 
                  ? 'bottom-full mb-1' 
                  : 'top-full mt-1'
              }`}
              onClick={(e) => e.stopPropagation()}
            >
              <Card className="p-4 shadow-lg border-border bg-card" onClick={(e) => e.stopPropagation()}>
                <div className="space-y-3">
                  {/* Header */}
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-medium text-foreground">
                      Select Primary LLM
                    </h3>
                    <div className="flex items-center gap-1">
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

                  {/* LLM List - Grouped by Provider */}
                  <div className="max-h-48 overflow-y-auto space-y-2 border-border border rounded-md p-2 bg-background">
                    {availableLLMs.length > 0 ? (
                      (() => {
                        // Group LLMs by provider
                        const groupedLLMs = availableLLMs.reduce((groups, llm) => {
                          if (!groups[llm.provider]) {
                            groups[llm.provider] = [];
                          }
                          groups[llm.provider].push(llm);
                          return groups;
                        }, {} as Record<string, LLMOption[]>);

                        return Object.entries(groupedLLMs).map(([provider, llms]) => (
                          <div key={provider} className="space-y-1">
                            {/* Provider Header */}
                            <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wide px-2 py-1 bg-secondary rounded">
                              {provider}
                            </div>
                            
                            {/* Provider's LLMs */}
                            {llms.map((llm) => (
                              <div 
                                key={`${llm.provider}-${llm.model}`}
                                className="flex items-center space-x-2 p-2 rounded-md hover:bg-secondary cursor-pointer ml-2"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  handleLLMSelect(llm);
                                  setIsOpen(false);
                                }}
                              >
                                <div className="flex-1">
                                  <div className="text-sm font-medium text-foreground">
                                    {llm.label}
                                  </div>
                                  {llm.description && (
                                    <div className="text-xs text-muted-foreground">
                                      {llm.description}
                                    </div>
                                  )}
                                  <div className="text-xs text-muted-foreground">
                                    {llm.model}
                                  </div>
                                </div>
                                {selectedLLM && selectedLLM.provider === llm.provider && selectedLLM.model === llm.model && (
                                  <Check className="w-4 h-4 text-primary" />
                                )}
                              </div>
                            ))}
                          </div>
                        ));
                      })()
                    ) : (
                      <div className="text-sm text-muted-foreground text-center py-4">
                        No LLMs available. Check your configuration.
                      </div>
                    )}
                  </div>

                  {/* Instructions */}
                  <div className="text-xs text-muted-foreground">
                    {selectedLLM 
                      ? `Primary LLM: ${selectedLLM.label}`
                      : 'No primary LLM selected - will use default'
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
