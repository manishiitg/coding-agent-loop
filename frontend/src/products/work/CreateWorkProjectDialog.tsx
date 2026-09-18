import { useState, type FormEvent } from 'react'
import { AlertCircle, FolderKanban, Loader2, Plus, X } from 'lucide-react'

export function CreateWorkProjectDialog({ onClose, onCreate, submitting, error }: {
  onClose: () => void
  onCreate: (title: string, description: string, icon?: string) => void | Promise<void>
  submitting: boolean
  error: string | null
}) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [icon, setIcon] = useState('')

  const submit = (event: FormEvent) => {
    event.preventDefault()
    const trimmedTitle = title.trim()
    if (!trimmedTitle || submitting) return
    void onCreate(trimmedTitle, description.trim(), icon.trim())
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-5 backdrop-blur-sm" role="presentation">
      <form onSubmit={submit} role="dialog" aria-modal="true" aria-labelledby="work-create-project-title" className="relative w-full max-w-md rounded-2xl border border-border bg-background p-6 shadow-2xl">
        <button type="button" onClick={onClose} disabled={submitting} aria-label="Close" className="absolute right-4 top-4 grid h-8 w-8 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50">
          <X className="h-4 w-4" />
        </button>
        <span className="grid h-11 w-11 place-items-center rounded-xl bg-primary/10 text-primary"><FolderKanban className="h-5 w-5" /></span>
        <h2 id="work-create-project-title" className="mt-4 text-xl font-semibold text-foreground">Create a Crew member</h2>
        <p className="mt-1.5 text-sm leading-6 text-muted-foreground">Give this project a clear name. Its chats, files, coding work, tools, schedules, and dashboard will stay together.</p>
        <div className="mt-5 grid grid-cols-[72px_minmax(0,1fr)] gap-3">
          <label className="block text-xs font-semibold text-foreground">
            Icon
            <input data-testid="work-create-project-icon-input" value={icon} onChange={event => setIcon(Array.from(event.target.value).slice(0, 8).join(''))} placeholder="🚀" className="mt-2 w-full rounded-lg border border-border bg-background px-3 py-3 text-center text-base font-normal outline-none focus:border-primary focus:ring-4 focus:ring-primary/10" />
          </label>
          <label className="block text-xs font-semibold text-foreground">
            Crew name
            <input autoFocus data-testid="work-create-project-name-input" value={title} onChange={event => setTitle(event.target.value)} maxLength={60} placeholder="Launch crew" className="mt-2 w-full rounded-lg border border-border bg-background px-3.5 py-3 text-sm font-normal outline-none focus:border-primary focus:ring-4 focus:ring-primary/10" />
          </label>
        </div>
        <label className="mt-4 block text-xs font-semibold text-foreground">
          Description <span className="font-normal text-muted-foreground">(optional)</span>
          <textarea value={description} onChange={event => setDescription(event.target.value)} maxLength={1000} rows={3} placeholder="What will you use this project for?" className="mt-2 w-full resize-none rounded-lg border border-border bg-background px-3.5 py-3 text-sm font-normal outline-none focus:border-primary focus:ring-4 focus:ring-primary/10" />
        </label>
        {error ? <p className="mt-3 flex items-start gap-2 text-xs text-destructive"><AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />{error}</p> : null}
        <div className="mt-6 flex justify-end gap-2">
          <button type="button" onClick={onClose} disabled={submitting} className="rounded-lg px-4 py-2.5 text-xs font-semibold text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50">Cancel</button>
          <button type="submit" data-testid="work-create-project-submit" disabled={!title.trim() || submitting} className="inline-flex min-w-32 items-center justify-center gap-2 rounded-lg bg-primary px-4 py-2.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50">
            {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
            {submitting ? 'Creating…' : 'Create Crew member'}
          </button>
        </div>
      </form>
    </div>
  )
}
