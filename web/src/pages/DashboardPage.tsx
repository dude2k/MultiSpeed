import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Activity, ArrowDown, ArrowUp, CalendarClock, CircleAlert, Gauge, Network, Plus, Radio, Router } from 'lucide-react'
import { Link } from 'react-router'
import { api } from '../lib/api'
import { queryKeys } from '../lib/query'
import { formatMilliseconds, formatRelative, providerColors } from '../lib/utils'
import type { DashboardTaskSummary, ResultSummary, StatisticBucket, StatisticsResponse, Task } from '../lib/types'
import { useAppSettings, useFormatters } from '../hooks/useAppSettings'
import { PageHeader, SectionHeader } from '../components/common/PageHeader'
import { MetricCard } from '../components/common/MetricCard'
import { ProviderBadge, ResultStatusBadge } from '../components/common/EntityBadges'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { EmptyState, ErrorState, LoadingState } from '../components/ui/states'
import { EChart, chartGridColor, chartTextColor, type ChartOption } from '../components/charts/EChart'
import { Badge, StatusDot } from '../components/ui/badge'

const ranges = [
  { label: '24h', hours: 24 },
  { label: '7d', hours: 24 * 7 },
  { label: '30d', hours: 24 * 30 },
  { label: '90d', hours: 24 * 90 },
] as const

export default function DashboardPage() {
  const settingsQuery = useAppSettings()
  const { bitrate, dateTime } = useFormatters()
  const [rangeOverride, setRangeOverride] = useState<(typeof ranges)[number] | null>(null)
  const [asOf, setAsOf] = useState(() => Date.now())
  const range = rangeOverride ?? ranges.find((item) => item.label === settingsQuery.settings.defaultChartRange) ?? ranges[2]
  const from = new Date(asOf - range.hours * 3_600_000).toISOString()
  const to = new Date(asOf).toISOString()
  const tasksQuery = useQuery({ queryKey: queryKeys.tasks, queryFn: api.tasks })
  const summaryQuery = useQuery({ queryKey: queryKeys.dashboardSummary, queryFn: api.dashboardSummary })
  const interfacesQuery = useQuery({ queryKey: [...queryKeys.interfaces, 'dashboard', true], queryFn: () => api.interfaces({ includeDown: true }) })
  const chartParams = { granularity: 'raw' as const, from, to, timezone: settingsQuery.settings.defaultTimezone, groupBy: 'task' }
  const chartQuery = useQuery({ queryKey: queryKeys.statistics({ dashboard: chartParams }), queryFn: () => api.statistics(chartParams) })

  if (tasksQuery.isLoading || summaryQuery.isLoading || interfacesQuery.isLoading || chartQuery.isLoading || settingsQuery.isLoading) return <LoadingState label="Assembling the live operations view…" />
  const firstError = tasksQuery.error ?? summaryQuery.error ?? interfacesQuery.error ?? settingsQuery.error
  if (firstError) return <ErrorState error={firstError} onRetry={() => { void tasksQuery.refetch(); void summaryQuery.refetch(); void interfacesQuery.refetch(); void chartQuery.refetch(); void settingsQuery.refetch() }} />

  const tasks = tasksQuery.data ?? []
  const summary = summaryQuery.data
  const interfaces = interfacesQuery.data ?? []
  const running = summary?.activeRuns ?? []
  const failures = summary?.recentFailures ?? []
  const failedTaskCount = summary?.failedTaskCount ?? 0
  const enabledTasks = tasks.filter((task) => task.enabled)
  const latestByTask = summary?.latestByTask ?? []
  const nextTasks = [...enabledTasks].filter((task) => task.nextScheduledAt).sort((a, b) => String(a.nextScheduledAt).localeCompare(String(b.nextScheduledAt))).slice(0, 5)
  const chartOption = throughputOption(chartQuery.data?.series, tasks, bitrate, dateTime)
  const hasChartData = chartQuery.data?.series?.some((item) => item.buckets.length > 0) ?? false
  const latestMeasurement = latestSuccessfulMeasurement(chartQuery.data?.series)
  const latestMeasurementTask = tasks.find((task) => task.id === latestMeasurement?.taskId)

  return (
    <>
      <PageHeader title="Every WAN, one operational picture." description="Live throughput, route integrity, and scheduled measurements across every configured network path." actions={<Button asChild><Link to="/tasks/new"><Plus className="h-4 w-4" />New task</Link></Button>} />

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="Latest download" value={bitrate(latestMeasurement?.bucket.download?.average)} detail={latestMeasurement ? `${latestMeasurementTask?.interfaceName ?? latestMeasurement.name} · ${formatRelative(bucketKey(latestMeasurement.bucket))}` : 'Waiting for a successful test'} icon={ArrowDown} tone="cyan" />
        <MetricCard label="Latest upload" value={bitrate(latestMeasurement?.bucket.upload?.average)} detail={latestMeasurementTask?.name ?? 'No measurement available'} icon={ArrowUp} tone="violet" />
        <MetricCard label="Current latency" value={formatMilliseconds(latestMeasurement?.bucket.latency?.average)} detail={latestMeasurementTask ? `${latestMeasurementTask.provider} methodology` : 'No measurement available'} icon={Gauge} tone="orange" />
        <MetricCard label="Task health" value={`${enabledTasks.length} enabled`} detail={failedTaskCount ? `${failedTaskCount} task${failedTaskCount === 1 ? '' : 's'} currently have a failed latest result` : 'No task has a failed latest result'} icon={failedTaskCount ? CircleAlert : Activity} tone={failedTaskCount ? 'rose' : 'emerald'} />
      </div>

      {running.length > 0 ? <RunningStrip results={running} /> : null}

      <div className="mt-6 grid gap-5 xl:grid-cols-[minmax(0,1.65fr)_minmax(320px,.75fr)]">
        <Card>
          <CardHeader className="flex-row items-center justify-between gap-3">
            <div><CardTitle>Throughput timeline</CardTitle><p className="mt-1 text-xs text-muted-foreground">Successful measurements by provider and path</p></div>
            <div className="flex rounded-lg border border-border bg-muted/50 p-1" aria-label="Chart range">
              {ranges.map((item) => <button key={item.label} type="button" onClick={() => { setRangeOverride(item); setAsOf(Date.now()) }} className={`rounded-md px-2.5 py-1 text-[11px] font-semibold transition ${range.label === item.label ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'}`} aria-pressed={range.label === item.label}>{item.label}</button>)}
            </div>
          </CardHeader>
          <CardContent>
            {chartQuery.error ? <ErrorState compact title="Throughput range is too large" error={chartQuery.error} onRetry={() => { void chartQuery.refetch() }} /> : hasChartData ? <EChart option={chartOption} ariaLabel={`Download and upload throughput over ${range.label}`} className="h-80" /> : <EmptyState compact title="No throughput in this range" description="Successful tests will appear here as soon as a task completes." />}
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle>Network readiness</CardTitle><p className="mt-1 text-xs text-muted-foreground">Interfaces visible in the active Linux namespace</p></CardHeader>
          <CardContent className="space-y-2">
            {interfaces.length === 0 ? <EmptyState compact title="No interfaces detected" description="Refresh interface discovery from Network & routes." /> : interfaces.slice(0, 6).map((item) => (
              <div key={item.name} className="flex items-center gap-3 rounded-lg border border-border bg-background p-3">
                <span className={`grid h-8 w-8 shrink-0 place-items-center rounded-lg ${item.operational ? 'bg-emerald-500/10 text-emerald-500' : 'bg-muted text-muted-foreground'}`}><Network className="h-4 w-4" /></span>
                <div className="min-w-0 flex-1"><div className="flex items-center gap-2"><p className="truncate text-sm font-semibold">{item.name}</p><Badge tone={item.operational ? 'success' : 'neutral'} className="gap-1"><StatusDot active={item.operational} />{item.operationalState || (item.operational ? 'Up' : 'Down')}</Badge></div><p className="mt-0.5 truncate text-[11px] text-muted-foreground">{item.addresses.filter((address) => !address.linkLocal).map((address) => address.address).join(' · ') || 'No routable address'}</p></div>
              </div>
            ))}
            <Button asChild variant="ghost" size="sm" className="mt-2 w-full"><Link to="/network">Inspect network paths</Link></Button>
          </CardContent>
        </Card>
      </div>

      <div className="mt-6 grid gap-5 xl:grid-cols-2">
        <section>
          <SectionHeader title="Latest by task and WAN" description="The newest state for every configured path, including tasks that have never run" />
          <Card className="overflow-hidden">
            {latestByTask.length === 0 ? <EmptyState title="No configured measurement paths" description="Create a task, validate its path, and run it to establish the first baseline." action={<Button asChild size="sm"><Link to="/tasks/new">Create a task</Link></Button>} /> : <div className="max-h-[42rem] divide-y divide-border overflow-y-auto scrollbar-thin">{latestByTask.map((entry) => <LatestPathRow key={entry.taskId} entry={entry} task={tasks.find((task) => task.id === entry.taskId)} formatBitrate={bitrate} />)}</div>}
          </Card>
        </section>
        <div className="grid gap-5 sm:grid-cols-2 xl:grid-cols-1 2xl:grid-cols-2">
          <section>
            <SectionHeader title="Next scheduled" description="Upcoming persisted schedules" />
            <Card className="overflow-hidden">
              {nextTasks.length === 0 ? <EmptyState compact title="Nothing scheduled" description="Enable a task to calculate its next run." /> : <div className="divide-y divide-border">{nextTasks.map((task) => <Link to={`/tasks/${task.id}/edit`} key={task.id} className="flex items-center gap-3 p-3.5 transition hover:bg-muted/50"><CalendarClock className="h-4 w-4 shrink-0 text-primary" /><div className="min-w-0 flex-1"><p className="truncate text-xs font-semibold">{task.name}</p><p className="mt-0.5 truncate text-[11px] text-muted-foreground">{dateTime(task.nextScheduledAt)} · {task.timezone}</p></div><ProviderBadge provider={task.provider} /></Link>)}</div>}
            </Card>
          </section>
          <section>
            <SectionHeader title="Recent failures" description="Actionable path and provider errors" />
            <Card className="overflow-hidden">
              {failures.length === 0 ? <EmptyState compact title="No recent failures" description="All tests in this range completed without a hard failure." /> : <div className="divide-y divide-border">{failures.slice(0, 5).map((result) => <Link to={`/results/${result.id}`} key={result.id} className="block p-3.5 transition hover:bg-muted/50"><div className="flex items-center justify-between gap-2"><p className="truncate text-xs font-semibold">{tasks.find((task) => task.id === result.taskId)?.name ?? 'Deleted task'}</p><span className="text-[10px] text-muted-foreground">{formatRelative(result.finishedAt)}</span></div><p className="mt-1 line-clamp-2 text-[11px] leading-4 text-rose-600 dark:text-rose-400">{result.sanitizedError || 'The provider did not complete the measurement.'}</p></Link>)}</div>}
            </Card>
          </section>
        </div>
      </div>
    </>
  )
}

function RunningStrip({ results }: { results: ResultSummary[] }) {
  return (
    <div className="mt-5 flex flex-col gap-3 rounded-xl border border-cyan-500/25 bg-cyan-500/[.06] p-4 sm:flex-row sm:items-center">
      <div className="flex h-9 items-center gap-1 rounded-lg bg-cyan-500/10 px-3" aria-hidden="true">{[0, 1, 2, 3].map((item) => <span key={item} className="h-5 w-1 animate-pulsebar rounded-full bg-cyan-500" style={{ animationDelay: `${item * 120}ms` }} />)}</div>
      <div className="min-w-0 flex-1"><p className="text-sm font-semibold text-foreground">{results.length} test{results.length === 1 ? '' : 's'} active</p><p className="truncate text-xs text-muted-foreground">{results.map((item) => `${item.selectedInterface || 'pending interface'} · ${item.status}`).join('  /  ')}</p></div>
      <Button asChild variant="outline" size="sm"><Link to="/results?status=running"><Radio className="h-3.5 w-3.5" />Follow live</Link></Button>
    </div>
  )
}

function LatestPathRow({ entry, task, formatBitrate }: { entry: DashboardTaskSummary; task?: Task | undefined; formatBitrate: (value: number | null | undefined, compact?: boolean) => string }) {
  const result = entry.latestResult
  const href = result ? `/results/${result.id}` : `/tasks/${entry.taskId}/edit`
  return (
    <Link to={href} className="grid gap-3 p-4 transition hover:bg-muted/45 sm:grid-cols-[minmax(0,1.2fr)_repeat(3,minmax(90px,.55fr))] sm:items-center">
      <div className="min-w-0"><div className="flex items-center gap-2"><Router className="h-4 w-4 shrink-0 text-primary" /><p className="truncate text-sm font-semibold">{entry.taskName}</p>{task ? <ProviderBadge provider={task.provider} /> : null}</div><p className="mt-1 truncate pl-6 text-[11px] text-muted-foreground">{entry.interfaceName} · {entry.sourceIp}{result?.detectedPublicIp ? ` · ${result.detectedPublicIp}` : ''}</p></div>
      <div><p className="text-[10px] uppercase tracking-wider text-muted-foreground">Download</p><p className="metric-number mt-0.5 text-sm font-semibold">{formatBitrate(result?.downloadBitsPerSecond)}</p></div>
      <div><p className="text-[10px] uppercase tracking-wider text-muted-foreground">Upload</p><p className="metric-number mt-0.5 text-sm font-semibold">{formatBitrate(result?.uploadBitsPerSecond)}</p></div>
      <div className="flex items-center justify-between gap-2 sm:block"><div><p className="text-[10px] uppercase tracking-wider text-muted-foreground">Latency</p><p className="metric-number mt-0.5 text-sm font-semibold">{formatMilliseconds(result?.latencyMilliseconds)}</p></div>{result ? <ResultStatusBadge status={result.status} /> : <Badge tone="neutral">Never run</Badge>}</div>
    </Link>
  )
}

function latestSuccessfulMeasurement(series: StatisticsResponse['series']): { taskId: string; name: string; bucket: StatisticBucket } | null {
  const samples = (series ?? []).flatMap((entry) => entry.buckets
    .filter((bucket) => bucket.successfulCount > 0 && bucket.download?.average != null)
    .map((bucket) => ({ taskId: entry.id, name: entry.name, bucket })))
  return samples.sort((left, right) => bucketKey(right.bucket).localeCompare(bucketKey(left.bucket)))[0] ?? null
}

function throughputOption(series: StatisticsResponse['series'], tasks: Task[], formatBitrate: (value: number | null | undefined, compact?: boolean) => string, formatDateTime: (value: string | null | undefined, withSeconds?: boolean) => string): ChartOption {
  const text = chartTextColor()
  const grid = chartGridColor()
  const entries = series ?? []
  const timestamps = [...new Set(entries.flatMap((entry) => entry.buckets.map(bucketKey)).filter(Boolean))].sort()
  const colors = [providerColors.ookla, providerColors.librespeed, providerColors.cloudflare, '#34d399', '#f472b6', '#facc15']
  const chartSeries = entries.flatMap((entry, index) => {
    const task = tasks.find((item) => item.id === entry.id)
    const label = task ? `${task.name} · ${task.interfaceName}` : entry.name
    const color = colors[index % colors.length] ?? providerColors.ookla
    const byTimestamp = new Map(entry.buckets.map((bucket) => [bucketKey(bucket), bucket]))
    return [
      { name: `${label} download`, type: 'line' as const, smooth: 0.18, showSymbol: timestamps.length < 25, symbolSize: 4, data: timestamps.map((timestamp) => byTimestamp.get(timestamp)?.download?.average ?? null), connectNulls: false, lineStyle: { width: 2, color }, itemStyle: { color } },
      { name: `${label} upload`, type: 'line' as const, smooth: 0.18, showSymbol: false, data: timestamps.map((timestamp) => byTimestamp.get(timestamp)?.upload?.average ?? null), connectNulls: false, lineStyle: { width: 1, type: 'dashed' as const, color }, itemStyle: { color } },
    ]
  })
  return {
    tooltip: { trigger: 'axis', backgroundColor: 'rgba(8,17,31,.94)', borderColor: 'rgba(148,163,184,.22)', textStyle: { color: '#e2e8f0', fontSize: 11 }, valueFormatter: (value) => formatBitrate(Number(value)) },
    legend: { right: 0, top: 0, type: 'scroll', textStyle: { color: text, fontSize: 10 } },
    grid: { left: 8, right: 12, top: 38, bottom: 8, containLabel: true },
    xAxis: { type: 'category', boundaryGap: false, data: timestamps.map((timestamp) => formatDateTime(timestamp)), axisLabel: { color: text, fontSize: 10, hideOverlap: true }, axisLine: { lineStyle: { color: grid } }, axisTick: { show: false } },
    yAxis: { type: 'value', axisLabel: { color: text, fontSize: 10, formatter: (value: number) => formatBitrate(value, true) }, splitLine: { lineStyle: { color: grid } } },
    series: chartSeries,
  }
}

function bucketKey(bucket: StatisticBucket): string {
  return bucket.bucket || bucket.timestamp || ''
}
