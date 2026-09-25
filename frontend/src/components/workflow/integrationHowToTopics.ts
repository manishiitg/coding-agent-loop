export const INTEGRATION_HOW_TO_TOPICS = ['MCPs', 'Skills', 'Slack', 'WhatsApp', 'Gmail', 'Connect'] as const
export type IntegrationHowToTopic = typeof INTEGRATION_HOW_TO_TOPICS[number]

export function integrationHowToTopic(topic: string): IntegrationHowToTopic | null {
  if (!topic.startsWith('Integrations · ')) return null
  const name = topic.slice('Integrations · '.length)
  return INTEGRATION_HOW_TO_TOPICS.find(item => item === name) ?? null
}
