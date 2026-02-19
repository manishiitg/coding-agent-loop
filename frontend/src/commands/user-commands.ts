import { createElement } from 'react'
import {
  Zap, Eye, Code, FileText, MessageCircle, Search, Bookmark, Star, Terminal
} from 'lucide-react'
import { commandsApi } from '../api/commands'
import type { UserCommand } from '../types/commands'
import type { CommandDefinition } from './types'
import type { ModeCategory } from '../stores/useModeStore'
import { setUserCommands } from './registry'

const iconComponents: Record<string, any> = {
  zap: Zap,
  eye: Eye,
  code: Code,
  'file-text': FileText,
  'message-circle': MessageCircle,
  search: Search,
  bookmark: Bookmark,
  star: Star,
  terminal: Terminal,
}

function makeIcon(name: string) {
  const Comp = iconComponents[name] || Terminal
  return createElement(Comp, { className: 'w-4 h-4' })
}

function toCommandDefinition(uc: UserCommand): CommandDefinition {
  const icon = makeIcon(uc.frontmatter.icon || 'terminal')

  const modes = uc.frontmatter.modes && uc.frontmatter.modes.length > 0
    ? uc.frontmatter.modes as ModeCategory[]
    : undefined

  return {
    command: uc.frontmatter.name,
    description: uc.frontmatter.description,
    icon,
    modes,
    source: 'user',
    execute: (ctx) => {
      let prompt = uc.content
      if (ctx.beforeSlash) {
        prompt = prompt.replace(/\{\{context\}\}/g, ctx.beforeSlash)
      } else {
        prompt = prompt.replace(/\{\{context\}\}/g, '')
      }
      prompt = prompt.trim()
      if (prompt) {
        ctx.onSubmit(prompt)
      }
    }
  }
}

export async function loadAndRegisterUserCommands(): Promise<void> {
  try {
    const response = await commandsApi.listCommands()
    const commands = (response.commands || []).map(toCommandDefinition)
    setUserCommands(commands)
  } catch {
    setUserCommands([])
  }
}
