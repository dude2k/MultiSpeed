import { describe, expect, it } from 'vitest'
import type { StatisticBucket, StatisticsResponse } from '../lib/types'
import { comparisonOption, summarizeWan } from './ComparisonPage'
import { throughputOption } from './DashboardPage'
import { statisticsOption } from './StatisticsPage'

const aggregate = (average: number | null) => ({ min: average, max: average, average, median: average, p95: average, standardDeviation: 0 })
const bucket = (at: string, average: number, counts = { samples: 1, succeeded: 1, failed: 0, skipped: 0 }): StatisticBucket => ({
  bucket: at,
  sampleCount: counts.samples,
  successfulCount: counts.succeeded,
  failedCount: counts.failed,
  skippedCount: counts.skipped,
  download: aggregate(average),
  upload: aggregate(average / 2),
  latency: aggregate(10),
})

describe('statistics and comparison data shaping', () => {
  it('plots dashboard throughput on a real time axis without gaps from unrelated tasks', () => {
    const series: NonNullable<StatisticsResponse['series']> = [
      { id: 'a', name: 'WAN A', buckets: [bucket('2026-01-01T00:00:00Z', 100), bucket('2026-01-03T00:00:00Z', 300)] },
      { id: 'b', name: 'WAN B', buckets: [bucket('2026-01-02T00:00:00Z', 200)] },
    ]
    const option = throughputOption(series, [], (value) => String(value), (value) => String(value))
    const xAxis = option.xAxis as { type?: string; data?: unknown[] }
    const tooltip = option.tooltip as { valueFormatter?: (value: unknown) => string }
    const chartSeries = option.series as Array<{ data: Array<[string, number]>; smooth?: boolean }>

    expect(xAxis.type).toBe('time')
    expect(xAxis).not.toHaveProperty('data')
    expect(chartSeries[0]?.data).toEqual([
      ['2026-01-01T00:00:00Z', 100],
      ['2026-01-03T00:00:00Z', 300],
    ])
    expect(chartSeries[2]?.data).toEqual([['2026-01-02T00:00:00Z', 200]])
    expect(chartSeries[0]?.smooth).toBe(false)
    expect(tooltip.valueFormatter?.(['2026-01-01T00:00:00Z', 100])).toBe('100')
  })

  it('aligns sparse grouped statistics by bucket start and leaves real gaps', () => {
    const series: NonNullable<StatisticsResponse['series']> = [
      { id: 'a', name: 'WAN A', buckets: [bucket('2026-01-01T00:00:00Z', 100), bucket('2026-01-03T00:00:00Z', 300)] },
      { id: 'b', name: 'WAN B', buckets: [bucket('2026-01-02T00:00:00Z', 200)] },
    ]
    const option = statisticsOption(series, [], 'download', (value) => String(value))
    const chartSeries = option.series as Array<{ data: Array<number | null> }>
    expect(chartSeries[0]?.data).toEqual([100, null, 300])
    expect(chartSeries[1]?.data).toEqual([null, 200, null])
  })

  it('uses exact overall attempt counts, including failures and skipped runs', () => {
    const overall = bucket('2026-01-01T00:00:00Z', 450, { samples: 10, succeeded: 6, failed: 3, skipped: 1 })
    const summary = summarizeWan({ id: 'eth0', name: 'eth0', overall, buckets: [bucket('2026-01-01T00:00:00Z', 100)] })
    expect(summary).toMatchObject({ samples: 10, succeeded: 6, failed: 3, skipped: 1, download: 450, successRate: 60 })
  })

  it('aligns every comparison metric on the same sparse bucket union', () => {
    const series: NonNullable<StatisticsResponse['series']> = [
      { id: 'a', name: 'WAN A', buckets: [bucket('2026-01-01T00:00:00Z', 100)] },
      { id: 'b', name: 'WAN B', buckets: [bucket('2026-01-02T00:00:00Z', 200)] },
    ]
    const option = comparisonOption(series, (value) => String(value), (value) => String(value))
    const chartSeries = option.series as Array<{ data: Array<number | null> }>
    expect(chartSeries[0]?.data).toEqual([100, null])
    expect(chartSeries[3]?.data).toEqual([null, 200])
  })
})
