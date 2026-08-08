import { describe, expect, it } from 'vitest'
import { formatBitrate, formatDateTime, reportingDateRange, safeExternalHttpUrl } from './utils'

describe('persisted presentation and reporting settings', () => {
  it('uses the reporting timezone and an exclusive next-day boundary across DST', () => {
    expect(reportingDateRange('2026-03-29', '2026-03-29', 'Europe/Berlin')).toEqual({
      from: '2026-03-28T23:00:00.000Z',
      to: '2026-03-29T22:00:00.000Z',
    })
  })

  it('formats throughput as bytes and instants in the selected IANA timezone', () => {
    expect(formatBitrate(800_000_000, false, 'bytes')).toBe('100 MB/s')
    expect(formatDateTime('2026-08-05T12:00:00Z', false, 'Asia/Kolkata')).toContain('17:30')
  })

  it('only accepts HTTP provider links', () => {
    expect(safeExternalHttpUrl('https://results.example.test/id/1')).toBe('https://results.example.test/id/1')
    expect(safeExternalHttpUrl('javascript:alert(1)')).toBeNull()
    expect(safeExternalHttpUrl('not a url')).toBeNull()
  })
})
