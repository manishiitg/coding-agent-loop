// Typed `#` reference tags for workflows and Crews.
//
// A bare `#Name` reads like a Slack channel to the agent, so the composer
// inserts `#crew:<label>` or `#workflow:<label>` and sends the same label with
// the reference so the server can list it under the name the user saw.

export type ReferenceKind = 'crew' | 'workflow'

export type ReferenceContextItem = { presetId: string; label: string; workspacePath: string }

export type WorkflowContextRef = { path: string; label: string; kind: ReferenceKind }

// Crew picker items use `crew:<projectId>` preset IDs; everything else is a workflow.
export function referenceKind(item: Pick<ReferenceContextItem, 'presetId'>): ReferenceKind {
  return item.presetId.startsWith('crew:') ? 'crew' : 'workflow'
}

export function referenceTag(item: Pick<ReferenceContextItem, 'presetId' | 'label'>): string {
  return `#${referenceKind(item)}:${item.label}`
}

// Tags this reference may appear as in the composer text: the typed tag and
// the legacy bare `#label` from drafts written before typed tags.
export function referenceTagForms(item: Pick<ReferenceContextItem, 'presetId' | 'label'>): string[] {
  return [referenceTag(item), `#${item.label}`]
}

export function textMentionsReference(text: string, item: Pick<ReferenceContextItem, 'presetId' | 'label'>): boolean {
  return referenceTagForms(item).some(tag => text.includes(tag))
}

export function removeReferenceTags(text: string, item: Pick<ReferenceContextItem, 'presetId' | 'label'>): string {
  let next = text
  for (const tag of referenceTagForms(item)) next = next.split(tag).join('')
  return next.replace(/  +/g, ' ').trim()
}

export function workflowContextRefs(items: ReferenceContextItem[]): WorkflowContextRef[] {
  return items.map(item => ({ path: item.workspacePath, label: item.label, kind: referenceKind(item) }))
}
