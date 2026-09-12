import type { ScheduledJob, ScheduledJobRun } from '../services/api-types'

const parseCronNumberField = (field: string, min: number, max: number): number[] | null => {
  const values = new Set<number>()
  for (const rawPart of field.split(',')) {
    const part = rawPart.trim()
    if (!part) return null
    const [rangePart, rawStep] = part.split('/')
    const step = rawStep ? Number(rawStep) : 1
    if (!Number.isInteger(step) || step <= 0) return null

    let start = min
    let end = max
    if (rangePart !== '*') {
      if (rangePart.includes('-')) {
        const [rawStart, rawEnd] = rangePart.split('-')
        start = Number(rawStart)
        end = Number(rawEnd)
      } else {
        start = Number(rangePart)
        end = start
      }
    }
    if (!Number.isInteger(start) || !Number.isInteger(end) || start < min || end > max || start > end) return null
    for (let value = start; value <= end; value += step) values.add(value)
  }
  return [...values].sort((left, right) => left - right)
}

const dailyCronMinutes = (expression?: string): number[] | null => {
  const parts = (expression || '').trim().split(/\s+/)
  if (parts.length !== 5 || parts.slice(2).some(part => part !== '*')) return null
  const minutes = parseCronNumberField(parts[0], 0, 59)
  const hours = parseCronNumberField(parts[1], 0, 23)
  if (!minutes || !hours) return null
  return hours.flatMap(hour => minutes.map(minute => hour * 60 + minute))
}

const zonedHourAndMinute = (date: Date, timeZone?: string): { hour: number; minute: number } | null => {
  try {
    const parts = new Intl.DateTimeFormat('en-GB', {
      timeZone: timeZone || undefined,
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    }).formatToParts(date)
    const hour = Number(parts.find(part => part.type === 'hour')?.value)
    const minute = Number(parts.find(part => part.type === 'minute')?.value)
    return Number.isFinite(hour) && Number.isFinite(minute) ? { hour, minute } : null
  } catch {
    return null
  }
}

const formatScheduleSlot = (date: Date, timeZone?: string): string => {
  try {
    return new Intl.DateTimeFormat([], {
      timeZone: timeZone || undefined,
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    }).format(date)
  } catch {
    return date.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }
}

const formatSlotOffset = (minutes: number): string => {
  if (minutes === 0) return 'on time'
  const absolute = Math.abs(minutes)
  const hours = Math.floor(absolute / 60)
  const remainder = absolute % 60
  const amount = [hours ? `${hours}h` : '', remainder ? `${remainder}m` : ''].filter(Boolean).join(' ')
  return minutes > 0 ? `+${amount}` : `${amount} early`
}

const inferredRunTriggerSource = (run: ScheduledJobRun): string => {
  if (run.trigger_source?.trim()) return run.trigger_source.trim().toLowerCase()
  if (run.session_id?.startsWith('schedule-manual--')) return 'manual'
  if (run.session_id?.startsWith('schedule-cron--')) return 'cron'
  return ''
}

const nearestDailyCronSlot = (job: ScheduledJob, startedAt: string): { at: Date; offsetMinutes: number } | null => {
  const started = new Date(startedAt)
  if (Number.isNaN(started.getTime())) return null
  const scheduleMinutes = dailyCronMinutes(job.cron_expression)
  const local = zonedHourAndMinute(started, job.timezone)
  if (!scheduleMinutes?.length || !local) return null
  const localMinute = local.hour * 60 + local.minute

  let closestOffset = Number.POSITIVE_INFINITY
  for (const scheduledMinute of scheduleMinutes) {
    const sameDayOffset = localMinute - scheduledMinute
    for (const offset of [sameDayOffset, sameDayOffset - 1440, sameDayOffset + 1440]) {
      if (Math.abs(offset) < Math.abs(closestOffset)) closestOffset = offset
    }
  }
  if (!Number.isFinite(closestOffset)) return null
  return {
    at: new Date(started.getTime() - closestOffset * 60_000),
    offsetMinutes: closestOffset,
  }
}

export const scheduleRunSlotLabel = (job: ScheduledJob, run: ScheduledJobRun): string | undefined => {
  if (job.schedule_type === 'webhook' || run.trigger_source === 'webhook' || run.session_id?.startsWith('schedule-webhook--')) return undefined
  if (run.scheduled_for) {
    const scheduledFor = new Date(run.scheduled_for)
    if (!Number.isNaN(scheduledFor.getTime())) return `Scheduled slot ${formatScheduleSlot(scheduledFor, job.timezone)}`
  }

  const triggerSource = inferredRunTriggerSource(run)
  const nearest = nearestDailyCronSlot(job, run.started_at)
  if (triggerSource === 'manual') {
    return nearest
      ? `Manual run · nearest slot ${formatScheduleSlot(nearest.at, job.timezone)} (${formatSlotOffset(nearest.offsetMinutes)})`
      : 'Manual run'
  }
  if (triggerSource === 'cron') {
    return nearest ? `Cron slot ${formatScheduleSlot(nearest.at, job.timezone)}` : 'Cron run'
  }
  return nearest ? `Nearest slot ${formatScheduleSlot(nearest.at, job.timezone)}` : undefined
}
