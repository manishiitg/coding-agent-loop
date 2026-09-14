// Shapes shared by the Bots panel and its children. API contract types
// (WhatsAppRoute, WhatsAppStatus, ChannelRoute) live in services/api-types.

export type ChannelKind = 'slack' | 'whatsapp'

// One route this workflow answers on, regardless of platform.
export type WorkflowRoute = {
  kind: ChannelKind
  key: string // Slack or WhatsApp slug
  workshop_mode?: string
  send_full_details?: boolean
  allowed_emails?: string[]
}

export const routeId = (route: { kind: ChannelKind; key: string }) => `${route.kind}:${route.key}`
