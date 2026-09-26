import type { ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'

export type HowToQuestion = {
  title: string
  answer: ReactNode
}

export function HowToAnswers({ topic, description, questions }: {
  topic: string
  description: string
  questions: HowToQuestion[]
}) {
  return (
    <section aria-label={`${topic} how-to answers`} className="mt-4 rounded-lg border border-border bg-muted/20 p-4">
      <h3 className="text-base font-semibold text-foreground">{topic}: how do I…?</h3>
      <p className="mt-1 text-sm text-muted-foreground">{description}</p>
      <div className="mt-4 divide-y divide-border rounded-md border border-border bg-background">
        {questions.map(question => (
          <details key={question.title} className="group px-4 py-3">
            <summary className="flex cursor-pointer list-none items-center gap-2 text-sm font-medium text-foreground [&::-webkit-details-marker]:hidden">
              <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
              {question.title}
            </summary>
            <p className="mt-3 pl-6 text-sm leading-6 text-muted-foreground">{question.answer}</p>
          </details>
        ))}
      </div>
    </section>
  )
}
