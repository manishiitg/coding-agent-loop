import React from 'react'
import { 
  Loader2, CheckCircle, XCircle, ArrowRight, 
  Code, GitBranch, Repeat, Zap, Lock, SkipForward, ShieldCheck,
  HelpCircle
} from 'lucide-react'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "../../ui/tooltip"

export const WorkflowLegend: React.FC = () => {
  const items = [
    { icon: <Loader2 className="w-3.5 h-3.5 text-blue-500 animate-spin" />, label: "Running Step" },
    { icon: <CheckCircle className="w-3.5 h-3.5 text-green-500" />, label: "Completed Step" },
    { icon: <XCircle className="w-3.5 h-3.5 text-red-500" />, label: "Failed Step" },
    { icon: <ArrowRight className="w-3.5 h-3.5 text-muted-foreground" />, label: "Pending Step" },
    { icon: <Code className="w-3.5 h-3.5 text-blue-500" />, label: "Code Execution Mode" },
    { icon: <Lock className="w-3.5 h-3.5 text-purple-500" />, label: "Learnings Locked" },
    { icon: <SkipForward className="w-3.5 h-3.5 text-indigo-500" />, label: "LLM Validation Skipped" },
    { icon: <ShieldCheck className="w-3.5 h-3.5 text-orange-500" />, label: "Validation Disabled" },
    { icon: <GitBranch className="w-3.5 h-3.5 text-purple-500" />, label: "Conditional Step" },
    { icon: <Repeat className="w-3.5 h-3.5 text-indigo-500" />, label: "Loop Step" },
    { icon: <Zap className="w-3.5 h-3.5 text-indigo-500" />, label: "Decision Step" },
          { icon: <GitBranch className="w-3.5 h-3.5 text-indigo-500" />, label: "Orchestrator Step" },    { 
      icon: <div className="w-3.5 h-3.5 rounded-full flex items-center justify-center text-[8px] font-semibold bg-green-500/20 text-green-700 dark:text-green-400">Y</div>, 
      label: "True/Yes Branch" 
    },
    { 
      icon: <div className="w-3.5 h-3.5 rounded-full flex items-center justify-center text-[8px] font-semibold bg-red-500/20 text-red-700 dark:text-red-400">N</div>, 
      label: "False/No Branch" 
    },
    { 
      icon: <div className="w-3.5 h-3.5 rounded-full flex items-center justify-center text-[8px] font-semibold bg-indigo-500/20 text-indigo-700 dark:text-indigo-400">S</div>, 
      label: "Sub-Agent" 
    },
  ]

  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button className="p-1 hover:bg-muted rounded-full transition-colors text-muted-foreground hover:text-foreground">
            <HelpCircle className="w-3.5 h-3.5" />
          </button>
        </TooltipTrigger>
        <TooltipContent side="right" className="p-0 border-border bg-popover/95 backdrop-blur-sm shadow-xl">
          <div className="p-3 grid grid-cols-2 gap-x-4 gap-y-2 min-w-[320px]">
            <div className="col-span-2 pb-2 mb-2 border-b border-border/50 text-xs font-semibold text-foreground/90">
              Workflow Status & Icons
            </div>
            {items.map((item, index) => (
              <div key={index} className="flex items-center gap-2 text-xs text-muted-foreground">
                <div className="flex-shrink-0 flex items-center justify-center w-5 h-5 bg-muted/30 rounded border border-border/30">
                  {item.icon}
                </div>
                <span>{item.label}</span>
              </div>
            ))}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
