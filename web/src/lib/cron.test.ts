import { afterEach, describe, expect, it, vi } from 'vitest'
import { describeCron, nextCronRuns, validateCron } from './cron'

describe('cron schedule helpers', () => {
  afterEach(() => vi.useRealTimers())
  it('validates standard five-field expressions', () => {
    expect(validateCron('*/15 * * * *')).toBe(true)
    expect(validateCron('0 6 * * 1')).toBe(true)
    expect(validateCron('60 25 * * *')).toBe(false)
    expect(validateCron('0 0 * *')).toBe(false)
  })

  it('describes presets and common custom intervals', () => {
    expect(describeCron('0 */6 * * *')).toBe('At 00:00, 06:00, 12:00, and 18:00')
    expect(describeCron('*/10 * * * *')).toBe('Every 10 minutes')
    expect(describeCron('0 8 * * *')).toBe('Daily at 08:00')
  })

  it('previews five timezone-aware future executions', async () => {
    const runs = await nextCronRuns('0 6 * * *', 'Europe/Berlin')
    expect(runs).toHaveLength(5)
    expect(runs.every((run, index) => index === 0 || run > (runs[index - 1] ?? run))).toBe(true)
  })

  it('keeps the selected wall-clock time across the Berlin DST transition', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-03-28T12:00:00Z'))
    const runs = await nextCronRuns('0 6 * * *', 'Europe/Berlin', 2)
    expect(runs.map((run) => run.toISOString())).toEqual(['2026-03-29T04:00:00.000Z', '2026-03-30T04:00:00.000Z'])
  })
})
