import React from 'react';

interface OrchestratorContextProps {
  metadata?: {
    [k: string]: unknown;
  };
  className?: string;
}

export const OrchestratorContext: React.FC<OrchestratorContextProps> = ({ 
  metadata, 
  className = "" 
}) => {
  // Debug: Log metadata to see what's being received
  React.useEffect(() => {
    console.log('OrchestratorContext - metadata received:', metadata);
  }, [metadata]);

  if (!metadata) {
    console.log('OrchestratorContext - no metadata provided');
    return null;
  }

  const orchestratorPhase = metadata.orchestrator_phase as string;
  const step = metadata.orchestrator_step as number;
  const iteration = metadata.orchestrator_iteration as number;
  const agentName = metadata.orchestrator_agent_name as string;

  console.log('OrchestratorContext - extracted values:', {
    orchestratorPhase,
    step,
    iteration,
    agentName
  });

  if (!orchestratorPhase) {
    console.log('OrchestratorContext - no orchestrator_phase found');
    return null;
  }

  const getPhaseInfo = (phase: string) => {
    const phases = {
      // Orchestrator phases
      'planning': { icon: '', name: 'Planning', color: 'blue' },
      'execution': { icon: '', name: 'Execution', color: 'green' },
      'validation': { icon: '', name: 'Validation', color: 'purple' },
      'organizer': { icon: '', name: 'Organization', color: 'orange' },
      // Workflow phases
      'todo_planner': { icon: '', name: 'Todo Planning', color: 'indigo' },
      'todo_execution': { icon: '', name: 'Todo Execution', color: 'emerald' },
      'todo_validation': { icon: '', name: 'Todo Validation', color: 'violet' },
      'workspace_update': { icon: '', name: 'Workspace Update', color: 'amber' },
      // Todo Planner Orchestrator phases (Planning & Todo Creation)
      'todo_planner_planning': { icon: '', name: 'Planning', color: 'blue' },
      'todo_planner_execution': { icon: '', name: 'Execution', color: 'green' },
      'todo_planner_validation': { icon: '', name: 'Basic Validation', color: 'purple' },
      'todo_planner_critique': { icon: '', name: 'Enhanced Critique', color: 'violet' },
      'todo_planner_writer': { icon: '', name: 'Writing', color: 'orange' },
      'todo_planner_cleanup': { icon: '', name: 'Cleanup', color: 'gray' }
    };
    return phases[phase as keyof typeof phases] || { icon: '', name: phase, color: 'gray' };
  };

  const getStepDisplay = (phase: string, step: number) => {
    // Todo Planner Orchestrator has 6 steps (including critique)
    if (phase.startsWith('todo_planner_')) {
      const stepMap = {
        'todo_planner_planning': 1,
        'todo_planner_execution': 2,
        'todo_planner_validation': 3,
        'todo_planner_critique': 4,
        'todo_planner_writer': 5,
        'todo_planner_cleanup': 6
      };
      const currentStep = stepMap[phase as keyof typeof stepMap] || step + 1;
      return `Step ${currentStep}/6`;
    }
    
    // Default step display
    return step !== undefined ? `Step ${step + 1}` : '';
  };

  const getIterationDisplay = (phase: string, iteration: number) => {
    // Todo Planner Orchestrator is iterative (up to 10 iterations)
    if (phase.startsWith('todo_planner_')) {
      return iteration !== undefined ? `Iteration ${iteration}/10` : '';
    }
    
    // Default iteration display
    return iteration !== undefined ? `Cycle ${iteration}` : '';
  };

  const phaseInfo = getPhaseInfo(orchestratorPhase);

  const getPhaseColorClasses = (color: string) => {
    const colorMap = {
      'blue': 'bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200',
      'green': 'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200',
      'purple': 'bg-purple-100 dark:bg-purple-900 text-purple-800 dark:text-purple-200',
      'orange': 'bg-orange-100 dark:bg-orange-900 text-orange-800 dark:text-orange-200',
      'indigo': 'bg-indigo-100 dark:bg-indigo-900 text-indigo-800 dark:text-indigo-200',
      'emerald': 'bg-emerald-100 dark:bg-emerald-900 text-emerald-800 dark:text-emerald-200',
      'violet': 'bg-violet-100 dark:bg-violet-900 text-violet-800 dark:text-violet-200',
      'amber': 'bg-amber-100 dark:bg-amber-900 text-amber-800 dark:text-amber-200',
      'gray': 'bg-gray-100 dark:bg-gray-800 text-gray-800 dark:text-gray-200'
    };
    return colorMap[color as keyof typeof colorMap] || colorMap.gray;
  };

  return (
    <div className={`text-xs text-gray-600 dark:text-gray-400 mb-2 flex items-center ${className}`}>
      <span className={`${getPhaseColorClasses(phaseInfo.color)} px-2 py-1 rounded mr-2 font-medium`}>
        {phaseInfo.name}
      </span>
      {agentName && (
        <span className="text-gray-500 dark:text-gray-500 mr-2">
          {agentName}
        </span>
      )}
      {step !== undefined && (
        <span className="text-gray-500 dark:text-gray-500 mr-2">
          {getStepDisplay(orchestratorPhase, step)}
        </span>
      )}
      {iteration !== undefined && (
        <span className="text-gray-500 dark:text-gray-500">
          {getIterationDisplay(orchestratorPhase, iteration)}
        </span>
      )}
    </div>
  );
};
