import { useEffect, useId, useRef, useState } from 'react'
import { X } from 'lucide-react'
import { pulseReviewFocuses } from '../../commands/pulse-review-focus'
import { Button } from '../ui/Button'
import ModalPortal from '../ui/ModalPortal'

interface PulseReviewFocusDialogProps {
  initialContext: string
  onClose: () => void
  onStart: (focusId: string, context: string) => void
}

export function PulseReviewFocusDialog({ initialContext, onClose, onStart }: PulseReviewFocusDialogProps) {
  const [focusId, setFocusId] = useState('auto')
  const [context, setContext] = useState(initialContext)
  const formRef = useRef<HTMLFormElement>(null)
  const selectRef = useRef<HTMLSelectElement>(null)
  const submitted = useRef(false)
  const id = useId()
  const selection = pulseReviewFocuses.find(focus => focus.id === focusId)

  useEffect(() => {
    const previousFocus = document.activeElement as HTMLElement | null
    selectRef.current?.focus()
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopPropagation()
        onClose()
      } else if (event.key === 'Tab') {
        const controls = formRef.current?.querySelectorAll<HTMLElement>('button:not(:disabled), select, textarea')
        if (!controls?.length) return
        const first = controls[0]
        const last = controls[controls.length - 1]
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault()
          last.focus()
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault()
          first.focus()
        }
      }
    }
    document.addEventListener('keydown', handleKeyDown, true)
    return () => {
      document.removeEventListener('keydown', handleKeyDown, true)
      if (previousFocus?.isConnected) previousFocus.focus()
    }
  }, [onClose])

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-[10000] flex items-center justify-center bg-black/50 p-4"
        data-workspace-collapse-ignore="true"
        onClick={event => { if (event.target === event.currentTarget) onClose() }}>
        <form ref={formRef} role="dialog" aria-modal="true" aria-labelledby={`${id}-title`} aria-describedby={`${id}-description`}
          className="max-h-[calc(100dvh-2rem)] w-full max-w-lg overflow-y-auto rounded-xl border border-border bg-background text-foreground shadow-xl"
          onKeyDown={event => event.stopPropagation()}
          onSubmit={event => {
            event.preventDefault()
            event.stopPropagation()
            if (submitted.current) return
            submitted.current = true
            onStart(focusId, context.trim())
          }}>
          <div className="flex items-start justify-between gap-4 border-b border-border p-5">
            <div>
              <h2 id={`${id}-title`} className="text-lg font-semibold">Technical review</h2>
              <p id={`${id}-description`} className="mt-1 text-sm text-muted-foreground">Choose where to start. The reviewer will investigate and apply bounded safe fixes.</p>
            </div>
            <Button type="button" variant="ghost" size="icon" onClick={onClose} aria-label="Close review focus picker"><X className="h-4 w-4" /></Button>
          </div>
          <div className="space-y-5 p-5">
            <div>
              <label htmlFor={`${id}-focus`} className="mb-2 block text-sm font-medium">Review focus</label>
              <select ref={selectRef} id={`${id}-focus`} value={focusId} onChange={event => setFocusId(event.target.value)}
                aria-describedby={`${id}-focus-description`}
                className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm focus:outline-none focus:ring-2 focus:ring-ring">
                <option value="auto">Let the reviewer decide</option>
                {pulseReviewFocuses.map(focus => <option key={focus.id} value={focus.id}>{focus.label}</option>)}
              </select>
              <p id={`${id}-focus-description`} className="mt-2 text-sm text-muted-foreground">
                {selection?.description ?? 'Choose useful investigations from the workflow’s outputs and meaningful concerns.'}
              </p>
            </div>
            <div>
              <label htmlFor={`${id}-context`} className="mb-2 block text-sm font-medium">Additional context <span className="font-normal text-muted-foreground">(optional)</span></label>
              <textarea id={`${id}-context`} value={context} onChange={event => setContext(event.target.value)} rows={3}
                placeholder="For example: check whether the latency report explains regressions clearly."
                className="w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring" />
            </div>
            <p className="text-xs text-muted-foreground">Shortcut: type /pulse-review database to start a focused review directly.</p>
          </div>
          <div className="flex justify-end gap-2 border-t border-border p-4">
            <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
            <Button type="submit">Start review and fixes</Button>
          </div>
        </form>
      </div>
    </ModalPortal>
  )
}
