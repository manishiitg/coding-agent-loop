import { useState, type FormEvent } from 'react'
import { AlertCircle, FolderKanban, Loader2, Plus, X } from 'lucide-react'
import { crewTemplates, type CrewTemplateId } from './crewTemplates'

export function CreateWorkProjectDialog({ onClose, onCreate, submitting, error }: {
  onClose: () => void
  onCreate: (title: string, description: string, icon?: string, templateId?: CrewTemplateId) => void | Promise<void>
  submitting: boolean
  error: string | null
}) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [icon, setIcon] = useState('')
  const [templateId, setTemplateId] = useState<CrewTemplateId | undefined>()
  const [templateSearch, setTemplateSearch] = useState('')
  const [templateCategory, setTemplateCategory] = useState('all')
  const template = crewTemplates.find(item => item.id === templateId)
  const categories = [...new Set(crewTemplates.map(item => item.category))]
  const matchingTemplates = crewTemplates.filter(item => (templateCategory === 'all' || item.category === templateCategory) && `${item.name} ${item.category} ${item.firstResult}`.toLowerCase().includes(templateSearch.toLowerCase()))

  const chooseTemplate = (id?: CrewTemplateId) => {
    const selected = crewTemplates.find(item => item.id === id)
    setTemplateId(id)
    setTitle(selected?.name || '')
    setDescription(selected?.purpose || '')
    setIcon(selected?.icon || '')
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    const trimmedTitle = title.trim()
    if (!trimmedTitle || (templateId && !description.trim()) || submitting) return
    void onCreate(trimmedTitle, description.trim(), icon.trim(), templateId)
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-5 backdrop-blur-sm" role="presentation">
      <form onSubmit={submit} role="dialog" aria-modal="true" aria-labelledby="work-create-project-title" className="relative max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-2xl border border-border bg-background p-6 shadow-2xl">
        <button type="button" onClick={onClose} disabled={submitting} aria-label="Close" className="absolute right-4 top-4 grid h-8 w-8 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50">
          <X className="h-4 w-4" />
        </button>
        <span className="grid h-11 w-11 place-items-center rounded-xl bg-primary/10 text-primary"><FolderKanban className="h-5 w-5" /></span>
        <h2 id="work-create-project-title" className="mt-4 text-xl font-semibold text-foreground">Create a Crew member</h2>
        <p className="mt-1.5 text-sm leading-6 text-muted-foreground">Start from a reusable template or create a blank Crew member. Its chats, files, tools, schedules, and dashboard stay together.</p>
        <fieldset className="mt-5 space-y-2">
          <legend className="text-xs font-semibold text-foreground">Start from</legend>
          <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-border p-3 text-sm hover:bg-muted/40">
            <input type="radio" name="crew-template" checked={!templateId} onChange={() => chooseTemplate()} className="mt-1" />
            <span><strong className="block text-foreground">Blank Crew</strong><span className="text-xs text-muted-foreground">Set up the role and skills yourself.</span></span>
          </label>
          <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
            <input aria-label="Search Crew templates" value={templateSearch} onChange={event => setTemplateSearch(event.target.value)} placeholder="Search templates" className="min-w-0 rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary" />
            <select aria-label="Filter template category" value={templateCategory} onChange={event => setTemplateCategory(event.target.value)} className="rounded-lg border border-border bg-background px-2 text-sm"><option value="all">All categories</option>{categories.map(category => <option key={category} value={category}>{category}</option>)}</select>
          </div>
          <div className="max-h-52 space-y-2 overflow-y-auto">{matchingTemplates.map(item => (
            <label key={item.id} className="flex cursor-pointer items-start gap-3 rounded-lg border border-border p-3 text-sm hover:bg-muted/40">
              <input data-testid={`work-template-${item.id}`} type="radio" name="crew-template" checked={templateId === item.id} onChange={() => chooseTemplate(item.id)} className="mt-1" />
              <span><strong className="block text-foreground">{item.icon} {item.name}</strong><span className="text-xs text-muted-foreground">{item.category} · {item.firstResult}</span></span>
            </label>
          ))}{matchingTemplates.length === 0 ? <p className="p-3 text-xs text-muted-foreground">No matching templates.</p> : null}</div>
        </fieldset>
        {template ? (
          <div className="mt-3 rounded-lg border border-border bg-muted/30 p-3 text-xs leading-5 text-muted-foreground">
            <p><strong className="text-foreground">To begin:</strong> {template.minimumInput}</p>
            <p className="mt-1"><strong className="text-foreground">Optional:</strong> {template.optionalConnections}</p>
            <p className="mt-1">The {template.name} skill and setup guide are included. You can add more templates in Crew Identity. Connections, schedules, triggers, functions, and Automations are not activated.</p>
            <p className="mt-2 font-semibold text-foreground">Try asking</p>
            <ul className="list-disc pl-4">{template.exampleRequests.map(request => <li key={request}>{request}</li>)}</ul>
          </div>
        ) : null}
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
          Description <span className="font-normal text-muted-foreground">{templateId ? '(Crew purpose)' : '(optional)'}</span>
          <textarea value={description} onChange={event => setDescription(event.target.value)} maxLength={1000} rows={3} placeholder="What will you use this project for?" className="mt-2 w-full resize-none rounded-lg border border-border bg-background px-3.5 py-3 text-sm font-normal outline-none focus:border-primary focus:ring-4 focus:ring-primary/10" />
        </label>
        {error ? <p className="mt-3 flex items-start gap-2 text-xs text-destructive"><AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />{error}</p> : null}
        <div className="mt-6 flex justify-end gap-2">
          <button type="button" onClick={onClose} disabled={submitting} className="rounded-lg px-4 py-2.5 text-xs font-semibold text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50">Cancel</button>
          <button type="submit" data-testid="work-create-project-submit" disabled={!title.trim() || (!!templateId && !description.trim()) || submitting} className="inline-flex min-w-32 items-center justify-center gap-2 rounded-lg bg-primary px-4 py-2.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50">
            {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
            {submitting ? 'Creating…' : 'Create Crew member'}
          </button>
        </div>
      </form>
    </div>
  )
}
