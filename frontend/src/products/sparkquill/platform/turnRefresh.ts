type TurnTab = { isStreaming: boolean; metadata?: { agentProfileId?: string } }

/** A completed SparkQuill turn may have edited an already-open workspace file. */
export function completedSparkQuillTurns(
  current: Record<string, TurnTab>,
  previous: Record<string, TurnTab>,
): { parent: boolean; child: boolean } {
  let parent = false
  let child = false
  for (const [id, tab] of Object.entries(current)) {
    if (!previous[id]?.isStreaming || tab.isStreaming) continue
    if (tab.metadata?.agentProfileId === 'sparkquill') parent = true
    if (tab.metadata?.agentProfileId === 'sparkquill-child') child = true
  }
  return { parent, child }
}
