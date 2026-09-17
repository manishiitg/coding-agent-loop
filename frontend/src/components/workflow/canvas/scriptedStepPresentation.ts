export function planStepTypeLabel(type: string): string {
  return type === 'regular' ? 'scripted step' : type
}
export function scriptedStepFilePath(stepId: string, codeLayoutVersion: number): string {
  return codeLayoutVersion >= 1
    ? `code/${stepId}/main.py`
    : `learnings/${stepId}/main.py`
}
