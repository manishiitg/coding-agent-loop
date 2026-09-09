import { useEffect, type RefObject } from 'react'

export interface ComposerPickerProps {
  inputRef?: RefObject<HTMLTextAreaElement | null>
  listId?: string
  onActiveOptionChange?: (id: string | undefined) => void
}

/** Keep focus in the composer and dismiss only when leaving this input/popup pair. */
export function useComposerPicker(
  isOpen: boolean,
  dialogRef: RefObject<HTMLDivElement | null>,
  inputRef: ComposerPickerProps['inputRef'],
  onClose: () => void,
) {
  useEffect(() => {
    if (!isOpen) return
    const dismissOutside = (event: Event) => {
      const target = event.target as Node | null
      if (target && !dialogRef.current?.contains(target) && target !== inputRef?.current) onClose()
    }
    window.addEventListener('resize', onClose)
    document.addEventListener('mousedown', dismissOutside)
    document.addEventListener('focusin', dismissOutside)
    return () => {
      window.removeEventListener('resize', onClose)
      document.removeEventListener('mousedown', dismissOutside)
      document.removeEventListener('focusin', dismissOutside)
    }
  }, [isOpen, dialogRef, inputRef, onClose])
}

export function isComposerPickerEvent(event: KeyboardEvent, inputRef: ComposerPickerProps['inputRef']) {
  // Legacy standalone callers have no input. Integrated pickers must never catch another input's keys.
  return !event.defaultPrevented && (!inputRef || event.target === inputRef.current)
}
