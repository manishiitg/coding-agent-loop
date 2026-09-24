import type { ReactNode } from 'react'
import { AlertCircle, CheckCircle } from 'lucide-react'
import type { SlackTestResponse } from '../../../services/api-types'

// Shared by a workflow's own bot form and the admin's shared bot form:
// how to create a Slack app, and the result of a token/scope test.

const Code = ({ children }: { children: ReactNode }) => (
  <code className="rounded bg-muted px-1 font-mono">{children}</code>
)

export function SlackAppSetupSteps({ finalStep }: { finalStep: ReactNode }) {
  return (
    <details className="rounded-md border border-border bg-muted/20 px-3 py-2 text-xs">
      <summary className="cursor-pointer select-none font-medium text-foreground">How to create a Slack app</summary>
      <ol className="mt-3 list-decimal space-y-3 pl-4 text-muted-foreground">
        <li>
          Go to <a href="https://api.slack.com/apps" target="_blank" rel="noreferrer" className="underline">api.slack.com/apps</a> → <b>Create New App</b> → <b>From scratch</b>. Pick a name and your workspace.
        </li>
        <li>
          <b>OAuth &amp; Permissions</b> → <b>Bot Token Scopes</b>. Add <Code>app_mentions:read</Code>, <Code>channels:history</Code>, <Code>groups:history</Code>, <Code>channels:read</Code>, <Code>groups:read</Code>, <Code>chat:write</Code>, <Code>chat:write.public</Code>, <Code>reactions:write</Code>, <Code>users:read</Code>, <Code>users:read.email</Code>, and optionally <Code>files:read</Code> for attachments.
        </li>
        <li>
          <b>Socket Mode</b> → enable it and create an App-Level Token with <Code>connections:write</Code>. It starts with <Code>xapp-</Code>: that is the <b>App Token</b>.
        </li>
        <li>
          <b>Event Subscriptions</b> → enable and subscribe to the bot events <Code>app_mention</Code>, <Code>message.channels</Code> and <Code>message.groups</Code>. All three are required; leave the Request URL empty.
        </li>
        <li>
          <b>Install App</b> → <b>Install to Workspace</b>. Copy the <b>Bot User OAuth Token</b> from OAuth &amp; Permissions. It starts with <Code>xoxb-</Code>: that is the <b>Bot Token</b>.
        </li>
        <li>{finalStep}</li>
      </ol>
      <p className="mt-3 italic text-muted-foreground">After changing scopes or events later, reinstall the app.</p>
    </details>
  )
}

export function SlackChecksView({ result }: { result: SlackTestResponse }) {
  return (
    <div className={`flex items-start gap-2 rounded-lg border p-3 ${result.success ? 'border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-900/20' : 'border-red-200 bg-red-50 dark:border-red-800 dark:bg-red-900/20'}`}>
      {result.success ? <CheckCircle className="mt-0.5 h-4 w-4 flex-shrink-0 text-green-600 dark:text-green-400" /> : <AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0 text-red-600 dark:text-red-400" />}
      <div className="min-w-0 space-y-2 text-sm">
        <p>{result.message}</p>
        {!!result.checks?.length && (
          <details open={!result.success}>
            <summary className="cursor-pointer font-medium">Setup checks</summary>
            <ul className="mt-2 space-y-2">
              {result.checks.map(check => (
                <li key={check.name}>
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium">{check.name}</span>
                    <span className={check.status === 'passed' ? 'text-green-600 dark:text-green-400' : check.status === 'manual' ? 'text-amber-600 dark:text-amber-400' : 'text-red-600 dark:text-red-400'}>
                      {check.status === 'manual' ? 'Verify manually' : check.status === 'passed' ? 'Passed' : check.status === 'missing' ? 'Missing' : 'Failed'}
                    </span>
                  </div>
                  {check.status !== 'passed' && <p className="text-xs text-muted-foreground">{check.message}</p>}
                </li>
              ))}
            </ul>
          </details>
        )}
      </div>
    </div>
  )
}
