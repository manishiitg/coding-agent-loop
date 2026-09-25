import { ChevronRight } from 'lucide-react'

type GmailHowToGuideProps = {
  scopeNoun: 'workflow' | 'project'
}

export function GmailHowToGuide({ scopeNoun }: GmailHowToGuideProps) {
  const questions = [
    {
      title: 'How do I connect my first account?',
      answer: <>Open the <b>First-time setup guide</b> below to prepare a Google Cloud project, OAuth consent screen, and client JSON. Then enter the mailbox address under <b>Sending accounts</b>, upload that JSON, choose any extra access you need, and click <b>Add &amp; sign in</b>. Finish the Google consent screen.</>,
    },
    {
      title: 'Which access should I choose?',
      answer: <>Notifications need only Gmail send access. <b>Reading this mailbox</b> allows search and reading; <b>agent drafts and send/reply</b> is a separate permission. Drive, Sheets, Docs, Slides, and Calendar are optional and start as read-only. Turn on write access for a service only when this {scopeNoun} must create or edit there.</>,
    },
    {
      title: 'How do I change access later?',
      answer: <>On the account row, click <b>Edit access</b>, change the Gmail or Google Workspace choices, then click <b>Reconnect with selected access</b> and complete Google consent. The <b>Currently authorized</b> badges show what Google actually granted; the account’s other badges show what was requested.</>,
    },
    {
      title: 'How do I add another mailbox?',
      answer: <>Under <b>Sending accounts</b>, click <b>+ Add account</b>, enter its address, upload the OAuth client JSON, and click <b>Add &amp; sign in</b>. If the Google app is in Testing, add the new address as a test user first. Set <b>Make default</b> on the sender you want used by default.</>,
    },
    {
      title: 'Which account sends, and who receives?',
      answer: <>Click <b>Make default</b> on a connected sending account. It is used unless work explicitly selects another connected account. In <b>Delivery settings</b>, enter comma-separated <b>Default recipients</b>, add any <b>Disallowed recipients</b>, and click <b>Save</b>.</>,
    },
    {
      title: 'How do I test delivery?',
      answer: <>Use <b>Send test</b> on an account row to test that sender. To test the account-wide delivery settings, enter a default recipient and click <b>Send test email</b>. If the test is unavailable, check that an account is connected and the recipient is not disallowed.</>,
    },
    {
      title: 'How do I pause email?',
      answer: <>Turn off <b>Enable Gmail</b> under <b>Delivery settings</b> and click <b>Save</b> to stop all outgoing notifications. To stop only one sender, use <b>Disable</b> on that account row. You can use <b>Enable</b> there when you need it again.</>,
    },
    {
      title: 'Why is Google sign-in blocked?',
      answer: <>If the Google app is still in <b>Testing</b>, add this mailbox under <b>Google Auth Platform → Audience → Test users</b>. For a hosted AgentWorks server, the OAuth client must be a <b>Web application</b> with the exact redirect URI shown in the First-time setup guide. Use <b>Copy link</b> if you need to sign in from a different Chrome profile.</>,
    },
    {
      title: 'Why did a new permission not take effect?',
      answer: <>Check <b>Currently authorized</b> on the account row. If a capability is still gray after reconnecting, add that exact scope under <b>Google Auth Platform → Data Access</b> in the same Cloud project, then reconnect again. Changing the checkboxes alone does not change a Google token.</>,
    },
    {
      title: 'Can people reply to notification emails?',
      answer: <>These emails are one-way notifications. A reply does not resume a {scopeNoun === 'project' ? 'Crew conversation' : 'workflow run'}. Mailbox reading and agent-authored replies require separate access and are configured above.</>,
    },
  ]

  return (
    <section aria-label="Gmail how-to answers" className="mt-4 rounded-lg border border-border bg-muted/20 p-4">
      <h3 className="text-base font-semibold text-foreground">Gmail: how do I…?</h3>
      <p className="mt-1 text-sm text-muted-foreground">Open a question for the steps you need. For full Google Cloud setup, expand First-time setup guide in the Gmail panel.</p>
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
