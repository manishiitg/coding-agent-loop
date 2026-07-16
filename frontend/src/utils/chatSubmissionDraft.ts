export interface SubmittedDraftState {
  accepted: boolean | void
  submittedTabId?: string
  currentTabId?: string | null
  submittedMessage: string
  currentMessage: string
}

export const shouldClearAcceptedChatDraft = ({
  accepted,
  submittedTabId,
  currentTabId,
  submittedMessage,
  currentMessage,
}: SubmittedDraftState): boolean => (
  accepted !== false
  && submittedTabId === (currentTabId || undefined)
  && submittedMessage === currentMessage
)
