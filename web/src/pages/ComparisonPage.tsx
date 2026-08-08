import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, CheckCircle2, Gauge, Trophy } from 'lucide-react'
import { api } from '../lib/api'
import { queryKeys } from '../lib/query'
import type { StatisticBucket, StatisticsResponse } from '../lib/types'
import { formatMilliseconds, formatPercent, providerColors } from '../lib/utils'
import { useAppSettings, useFormatters } from '../hooks/useAppSettings'
import { PageHeader } from '../components/common/PageHeader'
import { EChart, chartGridColor, chartTextColor, type ChartOption } from '../components/charts/EChart'
import { Badge } from '../components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { NativeSelect } from '../components/ui/fields'
import { EmptyState, ErrorState, LoadingState } from '../components/ui/states'

const palette = [providerColors.ookla, providerColors.librespeed, providerColors.cloudflare, '#34d399', '#f472b6', '#facc15']
const periods = [{ days: 1, label: 'Last 24 hours' }, { days: 7, label: 'Last 7 days' }, { days: 30, label: 'Last 30 days' }, { days: 90, label: 'Last 90 days' }, { days: 365, label: 'Last year' }] as const

export default function ComparisonPage() {
  const settingsQuery = useAppSettings()
  const { bitrate, dateTime } = useFormatters()
  const [daysOverride, setDaysOverride] = useState<number | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [asOf, setAsOf] = useState(() => Date.now())
  const days = daysOverride ?? chartRangeDays(settingsQuery.settings.defaultChartRange)
  const from = new Date(asOf - days * 86_400_000).toISOString()
  const to = new Date(asOf).toISOString()
  const interfacesQuery = useQuery({ queryKey: [...queryKeys.interfaces, 'comparison', true], queryFn: () => api.interfaces({ includeDown: true }) })
  const allInterfaces = interfacesQuery.data?.map((item) => item.name) ?? []
  const selectedWan = selected.length ? selected : allInterfaces.slice(0, 4)
  const statsParams = { granularity: 'day' as const, from, to, timezone: settingsQuery.settings.defaultTimezone, interfaceName: selectedWan, groupBy: 'interface' }
  const statisticsQuery = useQuery({ queryKey: queryKeys.statistics({ comparison: statsParams }), queryFn: () => api.statistics(statsParams), enabled: selectedWan.length > 0 })

  if (interfacesQuery.isLoading || settingsQuery.isLoading || (selectedWan.length > 0 && statisticsQuery.isLoading)) return <LoadingState label="Comparing independent WAN paths…" />
  const firstError = interfacesQuery.error ?? settingsQuery.error ?? statisticsQuery.error
  if (firstError) return <ErrorState error={firstError} onRetry={() => { void interfacesQuery.refetch(); void settingsQuery.refetch(); void statisticsQuery.refetch() }} />

  const summaries = (statisticsQuery.data?.series ?? []).flatMap((entry) => {
    const summary = summarizeWan(entry)
    return summary && summary.samples > 0 ? [summary] : []
  })
  const winner = [...summaries].sort((a, b) => scoreWan(b) - scoreWan(a))[0]
  const chart = comparisonOption(statisticsQuery.data?.series, bitrate, dateTime)

  return (
    <>
      <PageHeader title="Compare paths, not anecdotes." description="Use exact full-range statistics across successful, failed, skipped, and cancelled attempts to compare independent WAN paths without page truncation." actions={<NativeSelect className="w-44" value={days} onChange={(event) => { setDaysOverride(Number(event.target.value)); setAsOf(Date.now()) }}>{periods.map((period) => <option key={period.days} value={period.days}>{period.label}</option>)}</NativeSelect>} />
      <Card className="mb-5 p-4"><p className="mb-3 text-xs font-semibold">WAN paths to compare</p><div className="flex flex-wrap gap-2">{allInterfaces.map((name) => { const active = selectedWan.includes(name); return <button key={name} type="button" onClick={() => setSelected((current) => { const base = current.length ? current : allInterfaces.slice(0, 4); return base.includes(name) ? base.filter((item) => item !== name) : [...base, name] })} className={`inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-xs font-semibold transition ${active ? 'border-primary/40 bg-primary/10 text-primary' : 'border-border bg-background text-muted-foreground hover:text-foreground'}`} aria-pressed={active}>{active ? <CheckCircle2 className="h-3.5 w-3.5" /> : <span className="h-3.5 w-3.5 rounded-full border border-current" />}{name}</button>})}</div></Card>
      {summaries.length === 0 ? <Card><EmptyState title="No comparable WAN samples" description="Select at least one interface with measurement attempts in this reporting range." /></Card> : <>
        {winner ? <div className="mb-5 flex items-center gap-3 rounded-xl border border-emerald-500/25 bg-emerald-500/[.06] p-4"><span className="grid h-9 w-9 place-items-center rounded-lg bg-emerald-500/10 text-emerald-500"><Trophy className="h-4 w-4" /></span><div><p className="text-sm font-semibold">Best balanced path: {winner.name}</p><p className="text-xs text-muted-foreground">Highest normalized blend of exact download, upload, latency, and all-attempt success rate in this period.</p></div></div> : null}
        <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-4">{summaries.map((summary, index) => <Card key={summary.id} className="overflow-hidden"><div className="h-1" style={{ backgroundColor: palette[index % palette.length] }} /><div className="p-4"><div className="flex items-start justify-between"><div><h3 className="text-sm font-bold">{summary.name}</h3><p className="mt-0.5 text-[10px] text-muted-foreground">{summary.succeeded} succeeded · {summary.failed} failed · {summary.skipped} skipped</p></div><Badge tone={winner?.id === summary.id ? 'success' : 'neutral'}>{summary.samples} attempt{summary.samples === 1 ? '' : 's'}</Badge></div><dl className="mt-4 grid grid-cols-2 gap-3"><MiniMetric icon={ArrowDown} label="Download" value={bitrate(summary.download)} /><MiniMetric icon={ArrowUp} label="Upload" value={bitrate(summary.upload)} /><MiniMetric icon={Gauge} label="Latency" value={formatMilliseconds(summary.latency)} /><MiniMetric icon={CheckCircle2} label="Success" value={formatPercent(summary.successRate)} /></dl></div></Card>)}</div>
        <Card className="mt-5"><CardHeader><CardTitle>WAN performance timeline</CardTitle><p className="text-xs text-muted-foreground">Sparse interface series are aligned by reporting-bucket start; missing buckets remain gaps</p></CardHeader><CardContent><EChart option={chart} className="h-[28rem]" ariaLabel="WAN comparison for throughput and latency" /></CardContent></Card>
        <Card className="mt-5 overflow-hidden"><CardHeader><CardTitle>Comparison matrix</CardTitle></CardHeader><div className="overflow-x-auto"><table className="data-table w-full min-w-[930px]"><thead><tr><th>WAN</th><th>Attempts</th><th>Succeeded</th><th>Failed</th><th>Average download</th><th>Average upload</th><th>Average latency</th><th>Average jitter</th><th>Success rate</th></tr></thead><tbody>{summaries.map((summary) => <tr key={summary.id}><td className="font-semibold">{summary.name}</td><td>{summary.samples}</td><td>{summary.succeeded}</td><td>{summary.failed}</td><td>{bitrate(summary.download)}</td><td>{bitrate(summary.upload)}</td><td>{formatMilliseconds(summary.latency)}</td><td>{formatMilliseconds(summary.jitter)}</td><td>{formatPercent(summary.successRate)}</td></tr>)}</tbody></table></div></Card>
      </>}
    </>
  )
}

function MiniMetric({ icon: Icon, label, value }: { icon: typeof ArrowDown; label: string; value: string }) { return <div><p className="flex items-center gap-1 text-[10px] uppercase tracking-wider text-muted-foreground"><Icon className="h-3 w-3" />{label}</p><p className="metric-number mt-1 text-sm font-semibold">{value}</p></div> }

export function summarizeWan(entry: NonNullable<StatisticsResponse['series']>[number]) {
  const overall = entry.overall
  if (!overall) return null
  return {
    id: entry.id,
    name: entry.name,
    samples: overall.sampleCount,
    succeeded: overall.successfulCount,
    failed: overall.failedCount,
    skipped: overall.skippedCount,
    download: overall.download?.average ?? null,
    upload: overall.upload?.average ?? null,
    latency: overall.latency?.average ?? null,
    jitter: overall.jitter?.average ?? null,
    successRate: overall.successRate ?? (overall.sampleCount ? overall.successfulCount / overall.sampleCount * 100 : 0),
  }
}

function scoreWan(summary: NonNullable<ReturnType<typeof summarizeWan>>): number { return (summary.download ?? 0) / 1e8 + (summary.upload ?? 0) / 1e8 - (summary.latency ?? 100) / 50 + summary.successRate / 100 }

export function comparisonOption(series: StatisticsResponse['series'], formatBitrate: (value: number | null | undefined, compact?: boolean) => string, formatDateTime: (value: string | null | undefined, withSeconds?: boolean) => string): ChartOption {
  const text = chartTextColor()
  const grid = chartGridColor()
  const entries = series ?? []
  const timestamps = [...new Set(entries.flatMap((entry) => entry.buckets.map(bucketKey)).filter(Boolean))].sort()
  const chartSeries = entries.flatMap((entry, index) => {
    const byTimestamp = new Map(entry.buckets.map((bucket) => [bucketKey(bucket), bucket]))
    const color = palette[index % palette.length] ?? '#67e8f9'
    return [
      { name: `${entry.name} download`, type: 'line' as const, yAxisIndex: 0, smooth: .18, showSymbol: false, connectNulls: false, data: timestamps.map((timestamp) => byTimestamp.get(timestamp)?.download?.average ?? null), lineStyle: { width: 2, color }, itemStyle: { color } },
      { name: `${entry.name} upload`, type: 'line' as const, yAxisIndex: 0, smooth: .18, showSymbol: false, connectNulls: false, data: timestamps.map((timestamp) => byTimestamp.get(timestamp)?.upload?.average ?? null), lineStyle: { width: 1, type: 'dashed' as const, color }, itemStyle: { color } },
      { name: `${entry.name} latency`, type: 'line' as const, yAxisIndex: 1, smooth: .18, showSymbol: false, connectNulls: false, data: timestamps.map((timestamp) => byTimestamp.get(timestamp)?.latency?.average ?? null), lineStyle: { width: 1, type: 'dotted' as const, color }, itemStyle: { color } },
    ]
  })
  return { tooltip: { trigger: 'axis', backgroundColor: 'rgba(8,17,31,.95)', borderColor: 'rgba(148,163,184,.22)', textStyle: { color: '#e2e8f0', fontSize: 10 } }, legend: { top: 0, type: 'scroll', textStyle: { color: text, fontSize: 10 } }, grid: { left: 8, right: 12, top: 50, bottom: 8, containLabel: true }, xAxis: { type: 'category', boundaryGap: false, data: timestamps.map((timestamp) => formatDateTime(timestamp)), axisLabel: { color: text, fontSize: 10, hideOverlap: true }, axisLine: { lineStyle: { color: grid } } }, yAxis: [{ type: 'value', axisLabel: { color: text, fontSize: 10, formatter: (value: number) => formatBitrate(value, true) }, splitLine: { lineStyle: { color: grid } } }, { type: 'value', axisLabel: { color: text, fontSize: 10, formatter: (value: number) => `${value}ms` }, splitLine: { show: false } }], series: chartSeries }
}

function bucketKey(bucket: StatisticBucket): string { return bucket.bucket || bucket.timestamp || '' }
function chartRangeDays(value: string): number { return value === '24h' ? 1 : value === '7d' ? 7 : value === '90d' ? 90 : 30 }
