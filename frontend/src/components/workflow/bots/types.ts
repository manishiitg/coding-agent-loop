// Shapes shared by the Bots panel and its children. API contract types
// (WhatsAppRoute, WhatsAppStatus) live in services/api-types.

export type ChannelKind = 'slack' | 'whatsapp'

// One route this workflow answers on. Slack keys are channel IDs; WhatsApp
// keys are workflow slugs.
export type WorkflowRoute = {
  kind: ChannelKind
  key: string
  workshop_mode?: 'run' | 'workshop' | string
}

export const routeId = (route: { kind: ChannelKind; key: string }) => `${route.kind}:${route.key}`
