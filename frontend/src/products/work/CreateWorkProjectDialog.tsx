import { useState, type FormEvent } from 'react'
import { AlertCircle, Check, FolderKanban, Loader2, Plus, Search, Sparkles, X } from 'lucide-react'
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
  const matchingTemplates = crewTemplates.filter(item =>
    (templateCategory === 'all' || item.category === templateCategory)
    && `${item.name} ${item.category} ${item.firstResult} ${item.purpose}`.toLowerCase().includes(templateSearch.trim().toLowerCase()),
  )

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
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/65 p-3 backdrop-blur-sm sm:p-6" role="presentation">
      <form onSubmit={submit} role="dialog" aria-modal="true" aria-labelledby="work-create-project-title" className="flex max-h-[min(92vh,760px)] w-full max-w-[900px] flex-col overflow-hidden rounded-2xl border border-border bg-background shadow-2xl">
        <header className="flex shrink-0 items-start justify-between gap-4 border-b border-border px-5 py-4 sm:px-7 sm:py-5">
          <div className="flex items-start gap-3.5">
            <span className="grid h-10 w-10 shrink-0 place-items-center rounded-xl bg-primary/10 text-primary"><FolderKanban className="h-5 w-5" /></span>
            <div>
              <h2 id="work-create-project-title" className="text-lg font-semibold tracking-tight text-foreground sm:text-xl">Create a Crew member</h2>
              <p className="mt-0.5 text-sm text-muted-foreground">Pick a starting point, then make it yours.</p>
            </div>
          </div>
          <button type="button" onClick={onClose} disabled={submitting} aria-label="Close" className="grid h-8 w-8 shrink-0 place-items-center rounded-lg text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"><X className="h-4 w-4" /></button>
        </header>

        <div className="min-h-0 overflow-y-auto sm:grid sm:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)] sm:overflow-hidden">
          <section className="border-b border-border px-5 py-5 sm:flex sm:min-h-0 sm:flex-col sm:overflow-hidden sm:border-b-0 sm:border-r sm:px-6" aria-labelledby="crew-template-heading">
            <div className="mb-4 flex items-baseline justify-between gap-3">
              <h3 id="crew-template-heading" className="text-sm font-semibold text-foreground">Choose a template</h3>
              <span className="text-xs text-muted-foreground">{crewTemplates.length} available</span>
            </div>
            <div className="grid shrink-0 gap-2 min-[850px]:grid-cols-[minmax(0,1fr)_auto]">
              <label className="relative block">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <input aria-label="Search Crew templates" value={templateSearch} onChange={event => setTemplateSearch(event.target.value)} placeholder="Search templates" className="h-10 w-full min-w-0 rounded-lg border border-border bg-background pl-9 pr-3 text-sm outline-none placeholder:text-muted-foreground focus:border-primary focus:ring-2 focus:ring-primary/15" />
              </label>
              <select aria-label="Filter template category" value={templateCategory} onChange={event => setTemplateCategory(event.target.value)} className="h-10 w-full rounded-lg border border-border bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-2 focus:ring-primary/15 min-[850px]:w-auto"><option value="all">All categories</option>{categories.map(category => <option key={category} value={category}>{category}</option>)}</select>
            </div>
            <fieldset className="mt-4 space-y-2.5 sm:min-h-0 sm:flex-1 sm:overflow-y-auto sm:pr-1">
              <legend className="sr-only">Crew starting point</legend>
              <label className={`relative flex cursor-pointer items-center gap-3 rounded-xl border p-3.5 transition-colors focus-within:ring-2 focus-within:ring-primary/30 ${!templateId ? 'border-primary bg-primary/[0.07]' : 'border-border hover:border-primary/40 hover:bg-muted/40'}`}>
                <input type="radio" name="crew-template" checked={!templateId} onChange={() => chooseTemplate()} className="sr-only" />
                <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground"><Plus className="h-5 w-5" /></span>
                <span className="min-w-0 flex-1"><strong className="block text-sm font-semibold text-foreground">Blank Crew</strong><span className="mt-0.5 block text-xs text-muted-foreground">Build a role from scratch</span></span>
                {!templateId ? <Check className="h-4 w-4 shrink-0 text-primary" /> : null}
              </label>
              {matchingTemplates.map(item => (
                <label key={item.id} className={`relative flex cursor-pointer items-start gap-3 rounded-xl border p-3.5 transition-colors focus-within:ring-2 focus-within:ring-primary/30 ${templateId === item.id ? 'border-primary bg-primary/[0.07]' : 'border-border hover:border-primary/40 hover:bg-muted/40'}`}>
                  <input data-testid={`work-template-${item.id}`} type="radio" name="crew-template" checked={templateId === item.id} onChange={() => chooseTemplate(item.id)} className="sr-only" />
                  <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-muted text-xl" aria-hidden="true">{item.icon}</span>
                  <span className="min-w-0 flex-1"><span className="flex flex-wrap items-center gap-1.5"><strong className="text-sm font-semibold text-foreground">{item.name}</strong><span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">{item.category}</span></span><span className="mt-1 block text-xs leading-5 text-muted-foreground">{item.firstResult}</span></span>
                  {templateId === item.id ? <Check className="mt-1 h-4 w-4 shrink-0 text-primary" /> : null}
                </label>
              ))}
              {matchingTemplates.length === 0 ? <p className="rounded-xl border border-dashed border-border px-4 py-6 text-center text-sm text-muted-foreground">No templates match your search.</p> : null}
            </fieldset>
          </section>

          <section className="px-5 py-5 sm:min-h-0 sm:overflow-y-auto sm:px-6" aria-labelledby="crew-details-heading">
            <div className="mb-4 flex items-center gap-2">
              <h3 id="crew-details-heading" className="text-sm font-semibold text-foreground">Crew details</h3>
              {template ? <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-semibold text-primary">Template selected</span> : null}
            </div>
            {template ? (
              <div className="rounded-xl border border-border bg-muted/30 p-4">
                <div className="flex items-center gap-2.5"><span className="text-xl" aria-hidden="true">{template.icon}</span><strong className="text-sm text-foreground">{template.name}</strong></div>
                <p className="mt-2 text-xs leading-5 text-muted-foreground">{template.firstResult}</p>
                <div className="mt-3 grid gap-2 border-t border-border pt-3 text-xs leading-5 lg:grid-cols-2">
                  <div><span className="font-semibold text-foreground">To get started</span><p className="mt-0.5 text-muted-foreground">{template.minimumInput}</p></div>
                  <div><span className="font-semibold text-foreground">Connect later</span><p className="mt-0.5 text-muted-foreground">{template.optionalConnections}</p></div>
                </div>
                <p className="mt-3 flex items-start gap-1.5 border-t border-border pt-3 text-[11px] leading-4 text-muted-foreground"><Sparkles className="mt-0.5 h-3.5 w-3.5 shrink-0 text-primary" />The skill and setup checklist are included. Connections, schedules, triggers, functions, and Automations are not activated.</p>
              </div>
            ) : (
              <div className="rounded-xl border border-dashed border-border bg-muted/20 p-4 text-xs leading-5 text-muted-foreground">Start with an empty Crew member. You can add templates, skills, and tools in Crew Identity later.</div>
            )}
            <div className="mt-5 grid grid-cols-[64px_minmax(0,1fr)] gap-3">
              <label className="block text-xs font-semibold text-foreground">Icon<input data-testid="work-create-project-icon-input" value={icon} onChange={event => setIcon(Array.from(event.target.value).slice(0, 8).join(''))} placeholder="🚀" className="mt-2 h-11 w-full rounded-lg border border-border bg-background px-2 text-center text-base font-normal outline-none focus:border-primary focus:ring-2 focus:ring-primary/15" /></label>
              <label className="block text-xs font-semibold text-foreground">Crew name<input autoFocus data-testid="work-create-project-name-input" value={title} onChange={event => setTitle(event.target.value)} maxLength={60} placeholder="Give your Crew a name" className="mt-2 h-11 w-full rounded-lg border border-border bg-background px-3 text-sm font-normal outline-none focus:border-primary focus:ring-2 focus:ring-primary/15" /></label>
            </div>
            <label className="mt-4 block text-xs font-semibold text-foreground">Purpose <span className="font-normal text-muted-foreground">{templateId ? '' : '(optional)'}</span><textarea value={description} onChange={event => setDescription(event.target.value)} maxLength={1000} rows={3} placeholder="What should this Crew member help with?" className="mt-2 w-full resize-y rounded-lg border border-border bg-background px-3 py-2.5 text-sm font-normal outline-none focus:border-primary focus:ring-2 focus:ring-primary/15" /></label>
            {error ? <p className="mt-3 flex items-start gap-2 text-xs text-destructive"><AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />{error}</p> : null}
          </section>
        </div>

        <footer className="flex shrink-0 items-center justify-between gap-3 border-t border-border bg-background px-5 py-3.5 sm:px-7">
          <p className="hidden text-xs text-muted-foreground sm:block">You can add more templates after creation.</p>
          <div className="ml-auto flex items-center gap-2">
            <button type="button" onClick={onClose} disabled={submitting} className="rounded-lg px-3 py-2.5 text-xs font-semibold text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50">Cancel</button>
            <button type="submit" data-testid="work-create-project-submit" disabled={!title.trim() || (!!templateId && !description.trim()) || submitting} className="inline-flex min-w-32 items-center justify-center gap-2 rounded-lg bg-primary px-4 py-2.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50">{submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}{submitting ? 'Creating…' : 'Create Crew member'}</button>
          </div>
        </footer>
      </form>
    </div>
  )
}
