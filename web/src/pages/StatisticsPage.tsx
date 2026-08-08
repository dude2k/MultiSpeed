import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { BarChart3, CalendarRange, FilterX, Gauge, Sigma, Target } from 'lucide-react'
import { api } from '../lib/api'
import { queryKeys } from '../lib/query'
import type { MetricAggregate, ProviderId, StatisticBucket } from '../lib/types'
import { formatMilliseconds, formatPercent, providerColors, providerLabel, reportingDateRange } from '../lib/utils'
import { useAppSettings, useFormatters } from '../hooks/useAppSettings'
import { PageHeader } from '../components/common/PageHeader'
import { MetricCard } from '../components/common/MetricCard'
import { EChart, chartGridColor, chartTextColor, type ChartOption } from '../components/charts/EChart'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Input, NativeSelect } from '../components/ui/fields'
import { EmptyState, ErrorState, LoadingState, Spinner } from '../components/ui/states'

type Metric = 'download' | 'upload' | 'latency' | 'jitter' | 'packetLoss' | 'duration' | 'successRate'
type Group = 'raw' | 'day' | 'week' | 'month' | 'year'

const metrics: Array<{ id: Metric; label: string }> = [
  { id: 'download', label: 'Download throughput' }, { id: 'upload', label: 'Upload throughput' }, { id: 'latency', label: 'Latency' }, { id: 'jitter', label: 'Jitter' }, { id: 'packetLoss', label: 'Packet loss' }, { id: 'duration', label: 'Test duration' }, { id: 'successRate', label: 'Success rate' },
]

export default function StatisticsPage() {
  const settingsQuery = useAppSettings()
  const { bitrate } = useFormatters()
  const [asOf] = useState(() => Date.now())
  const [groupBy, setGroupBy] = useState<Group>('day')
  const [metric, setMetric] = useState<Metric>('download')
  const [dateOverride, setDateOverride] = useState<{ from: string; to: string } | null>(null)
  const defaultDates = datesForRange(asOf, settingsQuery.settings.defaultChartRange)
  const from = dateOverride?.from ?? defaultDates.from
  const to = dateOverride?.to ?? defaultDates.to
  const [taskIds, setTaskIds] = useState<string[]>([])
  const [interfaces, setInterfaces] = useState<string[]>([])
  const [providers, setProviders] = useState<string[]>([])
  const [serverIds, setServerIds] = useState<string[]>([])
  const [sourceIps, setSourceIps] = useState<string[]>([])
  const [routeProfileIds, setRouteProfileIds] = useState<string[]>([])
  const [publicIps, setPublicIps] = useState<string[]>([])
  const [dimension, setDimension] = useState('none')
  const reportRange = reportingDateRange(from, to, settingsQuery.settings.defaultTimezone)
  const tasksQuery = useQuery({ queryKey: queryKeys.tasks, queryFn: api.tasks })
  const interfacesQuery = useQuery({ queryKey: queryKeys.interfaces, queryFn: () => api.interfaces() })
  const routesQuery = useQuery({ queryKey: queryKeys.routes, queryFn: api.routes })
  const resultDimensionsQuery = useQuery({ queryKey: queryKeys.results({ dimensions: true, ...reportRange }), queryFn: () => api.results({ page: 1, pageSize: 200, ...reportRange }) })
  const statsParams = { granularity: (groupBy === 'week' ? 'iso-week' : groupBy), metric, ...reportRange, timezone: settingsQuery.settings.defaultTimezone, taskId: taskIds, interfaceName: interfaces, sourceIp: sourceIps, provider: providers, serverId: serverIds, routeProfileId: routeProfileIds, publicIp: publicIps, ...(dimension === 'none' ? {} : { groupBy: dimension }) } as const
  const statisticsQuery = useQuery({ queryKey: queryKeys.statistics(statsParams), queryFn: () => api.statistics(statsParams) })
  if (tasksQuery.isLoading || interfacesQuery.isLoading || routesQuery.isLoading || resultDimensionsQuery.isLoading || settingsQuery.isLoading) return <LoadingState label="Loading statistics controls…" />
  const supportingDataError = tasksQuery.error ?? interfacesQuery.error ?? routesQuery.error ?? resultDimensionsQuery.error ?? settingsQuery.error
  if (supportingDataError) return <ErrorState error={supportingDataError} onRetry={() => { void tasksQuery.refetch(); void interfacesQuery.refetch(); void routesQuery.refetch(); void resultDimensionsQuery.refetch(); void settingsQuery.refetch() }} />
  const response = statisticsQuery.data
  const buckets = response?.buckets ?? []
  const summary = response?.overall ?? response?.summary ?? response?.totals
  const aggregate = metricAggregate(summary, metric)
  const chartOption = statisticsOption(response?.series, buckets, metric, bitrate)
  const dimensionResults = resultDimensionsQuery.data?.items ?? []
  const servers = [...new Map(dimensionResults.filter((item) => item.serverId).map((item) => [item.serverId, item.serverName || item.serverId])).entries()]
  const sourceIpOptions = [...new Set(dimensionResults.map((item) => item.selectedSourceIp).filter(Boolean))].sort()
  const publicIpOptions = [...new Set(dimensionResults.map((item) => item.detectedPublicIp).filter(Boolean))].sort()
  const filtersActive = taskIds.length + interfaces.length + providers.length + serverIds.length + sourceIps.length + routeProfileIds.length + publicIps.length > 0
  const clearDimensions = () => { setTaskIds([]); setInterfaces([]); setProviders([]); setServerIds([]); setSourceIps([]); setRouteProfileIds([]); setPublicIps([]); setDimension('none') }
  const formatValue = metricFormatter(metric, bitrate)

  return (
    <>
      <PageHeader title="Patterns beyond a single test." description="Aggregate in the reporting timezone, compare dimensions, and inspect min, average, median, p95, and variance without failed sentinel values." />
      <Card className="mb-5">
        <div className="grid gap-4 p-4 lg:grid-cols-[1fr_1fr_1fr_1fr_auto]">
          <div><p className="mb-1.5 text-xs font-semibold">Aggregation</p><div className="flex flex-wrap rounded-lg border border-border bg-muted/40 p-1">{(['raw', 'day', 'week', 'month', 'year'] as Group[]).map((item) => <button key={item} type="button" onClick={() => setGroupBy(item)} className={`flex-1 rounded-md px-2 py-1.5 text-[11px] font-semibold capitalize transition ${groupBy === item ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}`} aria-pressed={groupBy === item}>{item === 'week' ? 'ISO week' : item}</button>)}</div></div>
          <div><p className="mb-1.5 text-xs font-semibold">Metric</p><NativeSelect value={metric} onChange={(event) => setMetric(event.target.value as Metric)}>{metrics.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}</NativeSelect></div>
          <div><p className="mb-1.5 text-xs font-semibold">From</p><Input type="date" value={from} max={to} onChange={(event) => setDateOverride({ from: event.target.value, to })} /></div>
          <div><p className="mb-1.5 text-xs font-semibold">To</p><Input type="date" value={to} min={from} onChange={(event) => setDateOverride({ from, to: event.target.value })} /></div>
          <div className="flex items-end"><Button variant="outline" onClick={() => void statisticsQuery.refetch()} disabled={statisticsQuery.isFetching}>{statisticsQuery.isFetching ? <Spinner /> : <CalendarRange className="h-4 w-4" />}Refresh</Button></div>
        </div>
        <div className="flex flex-wrap items-center gap-2 border-t border-border p-4">
          <MultiFilter label="Tasks" options={(tasksQuery.data ?? []).map((task) => ({ value: task.id, label: task.name }))} selected={taskIds} onChange={setTaskIds} />
          <MultiFilter label="WAN interfaces" options={(interfacesQuery.data ?? []).map((item) => ({ value: item.name, label: item.name }))} selected={interfaces} onChange={setInterfaces} />
          <MultiFilter label="Providers" options={(['ookla', 'librespeed', 'cloudflare'] as ProviderId[]).map((item) => ({ value: item, label: providerLabel(item) }))} selected={providers} onChange={setProviders} />
          <MultiFilter label="Servers" options={servers.map(([value, label]) => ({ value, label }))} selected={serverIds} onChange={setServerIds} />
          <MultiFilter label="Source IPs" options={sourceIpOptions.map((value) => ({ value, label: value }))} selected={sourceIps} onChange={setSourceIps} />
          <MultiFilter label="Route profiles" options={(routesQuery.data ?? []).map((route) => ({ value: route.id, label: route.name }))} selected={routeProfileIds} onChange={setRouteProfileIds} />
          <MultiFilter label="Public IPs" options={publicIpOptions.map((value) => ({ value, label: value }))} selected={publicIps} onChange={setPublicIps} />
          <NativeSelect className="h-9 w-52" value={dimension} onChange={(event) => setDimension(event.target.value)} aria-label="Group series by"><option value="none">Single series</option><option value="task">Group by task</option><option value="provider">Group by provider</option><option value="interface">Group by interface</option><option value="source-ip">Group by source IP</option><option value="server">Group by server</option><option value="route-profile">Group by route profile</option><option value="public-ip">Group by public IP</option></NativeSelect>
          <Button size="sm" variant="ghost" onClick={clearDimensions} disabled={!filtersActive && dimension === 'none'}><FilterX className="h-3.5 w-3.5" />Clear dimensions</Button>
        </div>
      </Card>
      {statisticsQuery.error ? <Card className="mt-5"><ErrorState compact title="Unable to calculate statistics" error={statisticsQuery.error} onRetry={() => { void statisticsQuery.refetch() }} /></Card> : statisticsQuery.isLoading ? <Card className="mt-5"><LoadingState compact label="Calculating statistical buckets…" /></Card> : <>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <MetricCard label="Average" value={formatValue(aggregate.average)} detail={`${summary?.successfulCount ?? 0} successful samples`} icon={BarChart3} tone="cyan" />
          <MetricCard label="Median" value={formatValue(aggregate.median)} detail="50th percentile" icon={Target} tone="violet" />
          <MetricCard label="P95" value={formatValue(aggregate.p95)} detail="95th percentile" icon={Gauge} tone="orange" />
          <MetricCard label="Standard deviation" value={formatValue(aggregate.standardDeviation)} detail={`${summary?.failedCount ?? 0} failed · ${summary?.skippedCount ?? 0} skipped`} icon={Sigma} tone="emerald" />
        </div>
        <Card className="mt-5"><CardHeader className="flex-row items-start justify-between"><div><CardTitle>{metrics.find((item) => item.id === metric)?.label} trend</CardTitle><p className="mt-1 text-xs text-muted-foreground">Min / average / max band with median and p95 · {response?.timezone ?? statsParams.timezone}</p></div>{statisticsQuery.isFetching ? <Spinner /> : null}</CardHeader><CardContent>{buckets.length || (response?.series?.length ?? 0) ? <EChart option={chartOption} ariaLabel={`${metrics.find((item) => item.id === metric)?.label} statistics by ${groupBy}`} className="h-[25rem]" /> : <EmptyState title="No statistical samples" description="No completed results match this date range and dimension filter." />}</CardContent></Card>
        <Card className="mt-5 overflow-hidden"><CardHeader><CardTitle>Accessible statistical table</CardTitle><p className="text-xs text-muted-foreground">Exact aggregate values for every reporting bucket</p></CardHeader>{buckets.length ? <div className="overflow-x-auto scrollbar-thin"><table className="data-table w-full min-w-[900px]"><thead><tr><th>Bucket</th><th>Samples</th><th>Succeeded</th><th>Failed</th><th>Minimum</th><th>Average</th><th>Median</th><th>P95</th><th>Maximum</th><th>Std. deviation</th></tr></thead><tbody>{buckets.map((bucket, index) => { const item = metricAggregate(bucket, metric); return <tr key={`${bucket.bucket}-${index}`}><td className="font-semibold">{bucket.label ?? bucket.bucket ?? bucket.timestamp}</td><td>{bucket.sampleCount}</td><td>{bucket.successfulCount}</td><td>{bucket.failedCount}</td><td>{formatValue(item.min)}</td><td>{formatValue(item.average)}</td><td>{formatValue(item.median)}</td><td>{formatValue(item.p95)}</td><td>{formatValue(item.max)}</td><td>{formatValue(item.standardDeviation)}</td></tr> })}</tbody></table></div> : <EmptyState compact title="No rows to display" description="Adjust the reporting period or dimension filters." />}</Card>
      </>}
    </>
  )
}

function MultiFilter({ label, options, selected, onChange }: { label: string; options: Array<{ value: string; label: string }>; selected: string[]; onChange: (values: string[]) => void }) {
  return <details className="group relative"><summary className="flex h-9 cursor-pointer list-none items-center gap-2 rounded-lg border border-border bg-background px-3 text-xs font-semibold transition hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring">{label}{selected.length ? <span className="grid min-w-5 place-items-center rounded-full bg-primary px-1.5 py-0.5 text-[10px] text-primary-foreground">{selected.length}</span> : null}</summary><div className="absolute left-0 top-11 z-40 max-h-64 min-w-52 overflow-y-auto rounded-lg border border-border bg-card p-2 shadow-xl scrollbar-thin">{options.length ? options.map((option) => <label key={option.value} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-xs hover:bg-muted"><input type="checkbox" checked={selected.includes(option.value)} onChange={() => onChange(selected.includes(option.value) ? selected.filter((value) => value !== option.value) : [...selected, option.value])} className="h-4 w-4 accent-cyan-500" />{option.label}</label>) : <p className="p-2 text-xs text-muted-foreground">No options available</p>}</div></details>
}

export function metricAggregate(bucket: StatisticBucket | undefined, metric: Metric): MetricAggregate {
  const empty = { min: null, max: null, average: null, median: null, p95: null, standardDeviation: null }
  if (!bucket) return empty
  if (metric === 'successRate') { const rate = typeof bucket.successRate === 'number' ? bucket.successRate : bucket.sampleCount ? bucket.successfulCount / bucket.sampleCount * 100 : null; return { min: rate, max: rate, average: rate, median: rate, p95: rate, standardDeviation: null } }
  const value = bucket[metric]
  if (typeof value === 'object' && value !== null) return { ...empty, ...(value as Partial<MetricAggregate>) }
  const aliases: Record<keyof MetricAggregate, string[]> = { min: [`${metric}Min`, 'min'], max: [`${metric}Max`, 'max'], average: [`${metric}Average`, 'average', 'avg'], median: [`${metric}Median`, 'median'], p95: [`${metric}P95`, 'p95'], standardDeviation: [`${metric}StandardDeviation`, 'standardDeviation', 'stdDev'] }
  return Object.fromEntries(Object.entries(aliases).map(([key, names]) => [key, names.map((name) => bucket[name]).find((item) => typeof item === 'number') ?? null])) as unknown as MetricAggregate
}

function metricFormatter(metric: Metric, formatBitrate: (value: number | null | undefined, compact?: boolean) => string): (value: number | null | undefined) => string {
  if (metric === 'download' || metric === 'upload') return formatBitrate
  if (metric === 'latency' || metric === 'jitter' || metric === 'duration') return formatMilliseconds
  if (metric === 'packetLoss' || metric === 'successRate') return formatPercent
  return (value) => value == null ? '—' : String(value)
}

export function statisticsOption(series: Array<{ id: string; name: string; buckets: StatisticBucket[] }> | undefined, buckets: StatisticBucket[], metric: Metric, formatBitrate: (value: number | null | undefined, compact?: boolean) => string): ChartOption {
  const text = chartTextColor(); const grid = chartGridColor(); const formatter = metricFormatter(metric, formatBitrate)
  const colors = [providerColors.ookla, providerColors.librespeed, providerColors.cloudflare, '#34d399', '#f472b6', '#facc15']
  const timestamps = series?.length ? [...new Set(series.flatMap((entry) => entry.buckets.map(bucketKey)).filter(Boolean))].sort() : buckets.map(bucketKey)
  const labelByTimestamp = new Map((series?.flatMap((entry) => entry.buckets) ?? buckets).map((bucket) => [bucketKey(bucket), bucket.label ?? bucketKey(bucket)]))
  const labels = timestamps.map((timestamp) => labelByTimestamp.get(timestamp) ?? timestamp)
  const chartSeries = series?.length ? series.flatMap((entry, index) => { const color = colors[index % colors.length] ?? '#67e8f9'; const byTimestamp = new Map(entry.buckets.map((bucket) => [bucketKey(bucket), bucket])); return [{ name: entry.name, type: 'line' as const, smooth: .18, showSymbol: entry.buckets.length < 30, symbolSize: 5, connectNulls: false, data: timestamps.map((timestamp) => metricAggregate(byTimestamp.get(timestamp), metric).average), lineStyle: { width: 2, color }, itemStyle: { color } }] }) : [
    { name: 'Minimum', type: 'line' as const, smooth: .18, showSymbol: false, data: buckets.map((bucket) => metricAggregate(bucket, metric).min), lineStyle: { width: 1, color: 'rgba(103,232,249,.38)' }, itemStyle: { color: providerColors.ookla } },
    { name: 'Average', type: 'line' as const, smooth: .18, showSymbol: buckets.length < 30, symbolSize: 5, data: buckets.map((bucket) => metricAggregate(bucket, metric).average), lineStyle: { width: 2.5, color: providerColors.ookla }, itemStyle: { color: providerColors.ookla }, areaStyle: { color: 'rgba(34,211,238,.08)' } },
    { name: 'P95', type: 'line' as const, smooth: .18, showSymbol: false, data: buckets.map((bucket) => metricAggregate(bucket, metric).p95), lineStyle: { width: 1.5, type: 'dashed' as const, color: providerColors.librespeed }, itemStyle: { color: providerColors.librespeed } },
    { name: 'Maximum', type: 'line' as const, smooth: .18, showSymbol: false, data: buckets.map((bucket) => metricAggregate(bucket, metric).max), lineStyle: { width: 1, color: 'rgba(167,139,250,.4)' }, itemStyle: { color: providerColors.librespeed } },
  ]
  return { tooltip: { trigger: 'axis', backgroundColor: 'rgba(8,17,31,.95)', borderColor: 'rgba(148,163,184,.22)', textStyle: { color: '#e2e8f0', fontSize: 11 }, valueFormatter: (value) => formatter(Number(value)) }, legend: { top: 0, right: 0, textStyle: { color: text, fontSize: 10 } }, grid: { left: 8, right: 12, top: 38, bottom: 8, containLabel: true }, xAxis: { type: 'category', boundaryGap: false, data: labels, axisLabel: { color: text, fontSize: 10, hideOverlap: true }, axisLine: { lineStyle: { color: grid } }, axisTick: { show: false } }, yAxis: { type: 'value', axisLabel: { color: text, fontSize: 10, formatter: (value: number) => formatter(value) }, splitLine: { lineStyle: { color: grid } } }, series: chartSeries }
}

function bucketKey(bucket: StatisticBucket): string { return bucket.bucket || bucket.timestamp || '' }

function datesForRange(asOf: number, value: string): { from: string; to: string } {
  const days = value === '24h' ? 1 : value === '7d' ? 7 : value === '90d' ? 90 : 30
  return { from: new Date(asOf - days * 86_400_000).toISOString().slice(0, 10), to: new Date(asOf).toISOString().slice(0, 10) }
}
