import { describe, expect, it } from 'vitest'
import type { ChatHistorySession, ScheduledJob, ScheduledJobRun } from '../services/api-types'
import { chatHistoryOpenDisposition } from '../utils/chatHistoryOpenDisposition'
import { isProviderTranscriptArtifact } from '../utils/restoredConversationFilter'
import { scheduleRunSlotLabel } from '../utils/scheduleRunSlot'
import { chatHistoryWorkshopMode } from '../utils/chatHistoryWorkshopMode'
import { chatHistoryRuntimeLabel, chatHistoryRuntimeShortLabel } from '../utils/chatHistoryRuntimeLabel'

function session(overrides: Partial<ChatHistorySession>): ChatHistorySession {
  return {
    session_id: 'chat-1',
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    ...overrides,
  }
}

describe('chatHistoryOpenDisposition', () => {
  it('keeps a scheduled coding-agent tmux session read-only', () => {
    expect(chatHistoryOpenDisposition(session({
      session_id: 'schedule-manual--37c45de4_1787214437488081000',
      runtime: {
        kind: 'coding_agent',
        transport: 'tmux',
        resume_supported: true,
      },
    }))).toBe('read-only-schedule')
  })

  it('resumes an ordinary retained coding-agent chat through its transport', () => {
    expect(chatHistoryOpenDisposition(session({
      session_id: 'chat-retained-1',
      runtime: {
        kind: 'coding_agent',
        transport: 'tmux',
        resume_supported: true,
      },
    }))).toBe('interactive-transport')
  })
})

describe('chat history workshop mode', () => {
  it('pins a saved Run chat to Run mode', () => {
    const runSession = session({
      workshop_mode: 'workshop',
      runtime: {
        kind: 'coding_agent',
        resume_supported: true,
        workshop_mode: 'run',
      },
    })
    expect(chatHistoryWorkshopMode(runSession)).toBe('run')
  })

  it('treats mode-less legacy chats as Workshop', () => {
    const legacySession = session({})
    expect(chatHistoryWorkshopMode(legacySession)).toBe('workshop')
  })
})

describe('chat history runtime labels', () => {
  it('keeps the complete provider/model available while using a compact agent label', () => {
    const museSession = session({
      runtime: {
        kind: 'coding_agent',
        provider: 'muse-cli',
        model_id: 'muse-spark-1.3-contributor',
        resume_supported: false,
      },
    })

    expect(chatHistoryRuntimeShortLabel(museSession)).toBe('Muse')
    expect(chatHistoryRuntimeLabel(museSession)).toBe('muse-cli · muse-spark-1.3-contributor')
  })
})

describe('restored schedule transcript', () => {
  it('does not render a canceled provider tool dump as assistant prose', () => {
    expect(isProviderTranscriptArtifact(
      '[Canceled run context — tools executed before cancellation:\n- execute_shell_command({"command":"git status"}) -> huge output]'
    )).toBe(true)
  })
})

describe('schedule execution slot labels', () => {
  const job: ScheduledJob = {
    id: 'daily-execution',
    name: 'Daily Execution x3',
    description: '',
    entity_type: 'workflow',
    cron_expression: '0 10,15,20 * * *',
    timezone: 'Asia/Kolkata',
    enabled: true,
    run_count: 1,
    consecutive_failures: 0,
  }

  const run: ScheduledJobRun = {
    id: 'run-1',
    job_id: job.id,
    status: 'error',
    started_at: '2026-08-21T05:46:17Z',
    session_id: 'schedule-manual--daily-execution_1787291177589416000',
  }

  it('makes a legacy manual run distinct from the closest scheduled slot', () => {
    const label = scheduleRunSlotLabel(job, run)
    expect(label).toContain('Manual run')
    expect(label).toContain('10:00')
    expect(label).toContain('+1h 16m')
  })

  it('uses the durable scheduled occurrence when the backend provides one', () => {
    const label = scheduleRunSlotLabel(job, {
      ...run,
      trigger_source: 'cron',
      scheduled_for: '2026-08-21T09:30:00Z',
      session_id: 'schedule-cron--daily-execution_1787304600000000000',
    })
    expect(label).toContain('Scheduled slot')
    expect(label).toContain('15:00')
  })
})
