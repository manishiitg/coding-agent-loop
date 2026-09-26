import { HowToAnswers } from '../HowToAnswers'

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

  return <HowToAnswers
    topic="Gmail"
    description="Open a question for the steps you need. For full Google Cloud setup, expand First-time setup guide in the Gmail panel."
    questions={questions}
  />
}
