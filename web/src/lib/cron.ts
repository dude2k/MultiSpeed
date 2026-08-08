export interface SchedulePreset {
  id: string
  label: string
  description: string
  expression: string
}

export const schedulePresets: SchedulePreset[] = [
  { id: '15m', label: 'Every 15 minutes', description: 'Four checks per hour', expression: '*/15 * * * *' },
  { id: 'hourly', label: 'Hourly', description: 'At the start of every hour', expression: '0 * * * *' },
  { id: 'six-hours', label: 'Every 6 hours', description: 'At 00:00, 06:00, 12:00, and 18:00', expression: '0 */6 * * *' },
  { id: 'daily', label: 'Daily', description: 'Every day at 06:00', expression: '0 6 * * *' },
  { id: 'weekly', label: 'Weekly', description: 'Every Monday at 06:00', expression: '0 6 * * 1' },
]

const cronPart = /^(?:\*|(?:\d+)(?:-\d+)?(?:\/\d+)?|\*\/\d+)(?:,(?:\d+)(?:-\d+)?(?:\/\d+)?)*$/

export function validateCron(expression: string): boolean {
  const parts = expression.trim().split(/\s+/)
  if (parts.length !== 5 || !parts.every((part) => cronPart.test(part))) return false
  const numeric = parts.map((part) => (/^\d+$/.test(part) ? Number(part) : null))
  return (numeric[0] == null || numeric[0] <= 59) &&
    (numeric[1] == null || numeric[1] <= 23) &&
    (numeric[2] == null || numeric[2] >= 1 && numeric[2] <= 31) &&
    (numeric[3] == null || numeric[3] >= 1 && numeric[3] <= 12) &&
    (numeric[4] == null || numeric[4] <= 7)
}

export function describeCron(expression: string): string {
  const preset = schedulePresets.find((item) => item.expression === expression)
  if (preset) return preset.description
  const parts = expression.trim().split(/\s+/)
  if (parts.length !== 5) return 'Invalid five-field cron expression'
  const [minute, hour, day, month, weekday] = parts
  if (minute?.startsWith('*/') && hour === '*' && day === '*' && month === '*' && weekday === '*') return `Every ${minute.slice(2)} minutes`
  if (minute === '0' && hour?.startsWith('*/') && day === '*' && month === '*' && weekday === '*') return `Every ${hour.slice(2)} hours`
  if (/^\d+$/.test(minute ?? '') && /^\d+$/.test(hour ?? '') && day === '*' && month === '*' && weekday === '*') {
    return `Daily at ${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
  }
  return validateCron(expression) ? `Custom schedule: ${expression}` : 'Invalid five-field cron expression'
}

export const commonTimezones = [
  'UTC',
  'Europe/Berlin',
  'Europe/London',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'Asia/Kolkata',
  'Asia/Singapore',
  'Asia/Tokyo',
  'Australia/Sydney',
]

export const ianaTimezones = (() => {
  try {
    const supportedValuesOf = (Intl as typeof Intl & { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf
    const discovered = supportedValuesOf?.('timeZone') ?? []
    return [...new Set([...commonTimezones, ...discovered])].sort((left, right) => left.localeCompare(right))
  } catch {
    return commonTimezones
  }
})()

export function isValidTimezone(value: string): boolean {
  try {
    new Intl.DateTimeFormat('en', { timeZone: value }).format()
    return true
  } catch {
    return false
  }
}

export async function nextCronRuns(expression: string, timezone: string, count = 5): Promise<Date[]> {
  if (!validateCron(expression)) return []
  try {
    const { CronExpressionParser } = await import('cron-parser')
    const interval = CronExpressionParser.parse(expression, { currentDate: new Date(), tz: timezone })
    return Array.from({ length: count }, () => interval.next().toDate())
  } catch {
    return []
  }
}
