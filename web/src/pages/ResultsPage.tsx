import { useCallback, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { columnVisibilityFeature, createColumnHelper, flexRender, tableFeatures, useTable, type Row } from '@tanstack/react-table'
import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp, Download, ExternalLink, FilterX, Trash2 } from 'lucide-react'
import { Link, useSearchParams } from 'react-router'
import { api, getApiErrorMessage, type ResultsQuery } from '../lib/api'
import { queryKeys } from '../lib/query'
import type { ProviderId, Result, ResultStatus } from '../lib/types'
import { downloadFromUrl, formatMilliseconds, formatPercent, providerLabel, reportingDateEndExclusive, reportingDateStart } from '../lib/utils'
import { useAppSettings, useFormatters } from '../hooks/useAppSettings'
import { PageHeader } from '../components/common/PageHeader'
import { ProviderBadge, ResultStatusBadge } from '../components/common/EntityBadges'
import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'
import { ConfirmDialog } from '../components/ui/dialog'
import { Input, NativeSelect } from '../components/ui/fields'
import { EmptyState, ErrorState, LoadingState, Spinner } from '../components/ui/states'
import { useToast } from '../components/ui/toast'

const resultTableFeatures = tableFeatures({ columnVisibilityFeature })
const columnHelper = createColumnHelper<typeof resultTableFeatures, Result>()

export default function ResultsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const { settings } = useAppSettings()
  const { bitrate, dateTime } = useFormatters()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const [sort, setSort] = useState('startedAt')
  const [direction, setDirection] = useState<'asc' | 'desc'>('desc')
  const [taskId, setTaskId] = useState(searchParams.get('taskId') ?? '')
  const [provider, setProvider] = useState(searchParams.get('provider') ?? '')
  const [network, setNetwork] = useState(searchParams.get('interfaceName') ?? '')
  const [status, setStatus] = useState(searchParams.get('status') ?? '')
  const [from, setFrom] = useState(searchParams.get('from') ?? '')
  const [to, setTo] = useState(searchParams.get('to') ?? '')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [confirmBatch, setConfirmBatch] = useState(false)
  const query: ResultsQuery = { page, pageSize, sort, direction, taskId, provider, interfaceName: network, status, from: from ? reportingDateStart(from, settings.defaultTimezone) : '', to: to ? reportingDateEndExclusive(to, settings.defaultTimezone) : '' }
  const querySignature = JSON.stringify(query)
  const resultsQuery = useQuery({ queryKey: queryKeys.results(query), queryFn: () => api.results(query), placeholderData: (previous) => previous })
  const tasksQuery = useQuery({ queryKey: queryKeys.tasks, queryFn: api.tasks })
  const interfacesQuery = useQuery({ queryKey: queryKeys.interfaces, queryFn: () => api.interfaces() })
  const deleteMutation = useMutation({
    mutationFn: api.deleteResults,
    onSuccess: (result) => { setSelected(new Set()); setConfirmBatch(false); void queryClient.invalidateQueries({ queryKey: ['results'] }); void queryClient.invalidateQueries({ queryKey: ['statistics'] }); toast({ tone: 'success', title: 'Results deleted', description: `${result.deleted} diagnostic record${result.deleted === 1 ? '' : 's'} removed.` }) },
    onError: (error) => toast({ tone: 'error', title: 'Unable to delete results', description: getApiErrorMessage(error) }),
  })

  const tasks = useMemo(() => tasksQuery.data ?? [], [tasksQuery.data])
  const items = useMemo(() => resultsQuery.data?.items ?? [], [resultsQuery.data?.items])
  const allVisibleSelected = items.length > 0 && items.every((item) => selected.has(item.id))
  const toggleAll = useCallback(() => setSelected((current) => {
    const next = new Set(current)
    if (allVisibleSelected) items.forEach((item) => next.delete(item.id)); else items.forEach((item) => next.add(item.id))
    return next
  }), [allVisibleSelected, items])
  const toggleExpanded = useCallback((id: string) => setExpanded((current) => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next }), [])
  const applySort = useCallback((key: string) => { if (sort === key) setDirection((current) => current === 'asc' ? 'desc' : 'asc'); else { setSort(key); setDirection('desc') } setPage(1) }, [sort])

  const columns = useMemo(() => columnHelper.columns([
    columnHelper.display({ id: 'select', header: () => <input type="checkbox" checked={allVisibleSelected} onChange={toggleAll} aria-label="Select all visible results" className="h-4 w-4 rounded border-input accent-cyan-500" />, cell: ({ row }) => <input type="checkbox" checked={selected.has(row.original.id)} onChange={() => setSelected((current) => { const next = new Set(current); if (next.has(row.original.id)) next.delete(row.original.id); else next.add(row.original.id); return next })} aria-label={`Select result ${row.original.id}`} className="h-4 w-4 rounded border-input accent-cyan-500" /> }),
    columnHelper.accessor('startedAt', { header: () => <SortButton label="Started" column="startedAt" sort={sort} direction={direction} onSort={applySort} />, cell: ({ getValue }) => <div className="whitespace-nowrap"><p className="text-xs font-medium">{dateTime(getValue())}</p><p className="mt-0.5 text-[10px] capitalize text-muted-foreground">{dateTime(getValue(), true)} · {items.find((item) => item.startedAt === getValue())?.trigger}</p></div> }),
    columnHelper.accessor('taskId', { header: 'Task', cell: ({ row }) => <div className="min-w-40"><Link to={`/results/${row.original.id}`} className="font-semibold text-foreground hover:text-primary hover:underline">{tasks.find((task) => task.id === row.original.taskId)?.name ?? 'Deleted task'}</Link><p className="mt-0.5 text-[10px] text-muted-foreground">{row.original.serverName || row.original.cloudflareColo || 'Automatic target'}</p></div> }),
    columnHelper.accessor('status', { header: 'Status', cell: ({ getValue }) => <ResultStatusBadge status={getValue()} /> }),
    columnHelper.accessor('provider', { header: 'Provider', cell: ({ getValue }) => <ProviderBadge provider={getValue()} /> }),
    columnHelper.accessor('selectedInterface', { header: 'WAN path', cell: ({ row }) => <div><p className="text-xs font-semibold">{row.original.selectedInterface || 'Pending'}</p><p className="mt-0.5 max-w-40 truncate font-mono text-[10px] text-muted-foreground">{row.original.selectedSourceIp || 'Source pending'}</p></div> }),
    columnHelper.accessor('downloadBitsPerSecond', { header: () => <SortButton label="Download" column="download" sort={sort} direction={direction} onSort={applySort} />, cell: ({ getValue }) => <span className="metric-number whitespace-nowrap text-xs font-semibold">{bitrate(getValue())}</span> }),
    columnHelper.accessor('uploadBitsPerSecond', { header: () => <SortButton label="Upload" column="upload" sort={sort} direction={direction} onSort={applySort} />, cell: ({ getValue }) => <span className="metric-number whitespace-nowrap text-xs font-semibold">{bitrate(getValue())}</span> }),
    columnHelper.accessor('latencyMilliseconds', { header: () => <SortButton label="Latency" column="latency" sort={sort} direction={direction} onSort={applySort} />, cell: ({ getValue }) => <span className="metric-number whitespace-nowrap text-xs font-semibold">{formatMilliseconds(getValue())}</span> }),
    columnHelper.display({ id: 'expand', header: '', cell: ({ row }) => <Button size="icon" variant="ghost" aria-label={`${expanded.has(row.original.id) ? 'Hide' : 'Show'} diagnostics`} onClick={() => toggleExpanded(row.original.id)}>{expanded.has(row.original.id) ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}</Button> }),
  ]), [allVisibleSelected, applySort, bitrate, dateTime, direction, expanded, items, selected, sort, tasks, toggleAll, toggleExpanded])
  const table = useTable({ features: resultTableFeatures, data: items, columns })

  if (resultsQuery.isLoading || tasksQuery.isLoading || interfacesQuery.isLoading) return <LoadingState label="Querying persisted measurements…" />
  const firstError = resultsQuery.error ?? tasksQuery.error ?? interfacesQuery.error
  if (firstError) return <ErrorState error={firstError} onRetry={() => { void resultsQuery.refetch(); void tasksQuery.refetch(); void interfacesQuery.refetch() }} />

  const clearFilters = () => { setTaskId(''); setProvider(''); setNetwork(''); setStatus(''); setFrom(''); setTo(''); setPage(1); setSearchParams({}) }
  const filtersActive = Boolean(taskId || provider || network || status || from || to)
  const exportResults = (format: 'csv' | 'json') => downloadFromUrl(api.exportUrl(format, query), `multispeed-results.${format}`)

  return (
    <>
      <PageHeader title="Every sample, fully explainable." description="Filter measurement history, inspect route and provider diagnostics, or export exactly the selected result set." actions={<><Button variant="outline" onClick={() => exportResults('csv')}><Download className="h-4 w-4" />CSV</Button><Button variant="outline" onClick={() => exportResults('json')}><Download className="h-4 w-4" />JSON</Button></>} />
      <Card className="overflow-hidden">
        <div className="grid gap-3 border-b border-border p-4 sm:grid-cols-2 xl:grid-cols-6">
          <NativeSelect value={taskId} onChange={(event) => { setTaskId(event.target.value); setPage(1) }} aria-label="Filter results by task"><option value="">All tasks</option>{tasks.map((task) => <option key={task.id} value={task.id}>{task.name}</option>)}</NativeSelect>
          <NativeSelect value={provider} onChange={(event) => { setProvider(event.target.value); setPage(1) }} aria-label="Filter results by provider"><option value="">All providers</option>{(['ookla', 'librespeed', 'cloudflare'] as ProviderId[]).map((item) => <option key={item} value={item}>{providerLabel(item)}</option>)}</NativeSelect>
          <NativeSelect value={network} onChange={(event) => { setNetwork(event.target.value); setPage(1) }} aria-label="Filter results by WAN"><option value="">All WAN paths</option>{interfacesQuery.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</NativeSelect>
          <NativeSelect value={status} onChange={(event) => { setStatus(event.target.value); setPage(1) }} aria-label="Filter results by status"><option value="">Any status</option>{(['queued', 'validating', 'running', 'succeeded', 'failed', 'skipped', 'cancelled'] as ResultStatus[]).map((item) => <option key={item} value={item}>{item[0]?.toUpperCase()}{item.slice(1)}</option>)}</NativeSelect>
          <Input type="date" value={from} onChange={(event) => { setFrom(event.target.value); setPage(1) }} aria-label="Results from date" />
          <div className="flex gap-2"><Input type="date" value={to} onChange={(event) => { setTo(event.target.value); setPage(1) }} aria-label="Results to date" /><Button size="icon" variant="ghost" disabled={!filtersActive} onClick={clearFilters} aria-label="Clear result filters"><FilterX className="h-4 w-4" /></Button></div>
        </div>
        {selected.size > 0 ? <div className="flex items-center justify-between gap-3 border-b border-border bg-primary/[.055] px-4 py-2.5"><p className="text-xs font-semibold">{selected.size} selected</p><Button size="sm" variant="danger" onClick={() => setConfirmBatch(true)}><Trash2 className="h-3.5 w-3.5" />Delete selected</Button></div> : null}
        {items.length === 0 ? <EmptyState title={filtersActive ? 'No results match these filters' : 'No measurements yet'} description={filtersActive ? 'Expand the date range or clear one or more filters.' : 'Completed and failed task runs will appear here with full diagnostics.'} action={filtersActive ? <Button variant="outline" size="sm" onClick={clearFilters}><FilterX className="h-3.5 w-3.5" />Clear filters</Button> : null} /> : <div className="overflow-x-auto scrollbar-thin"><table className="data-table w-full min-w-[1160px]"><thead>{table.getHeaderGroups().map((group) => <tr key={group.id}>{group.headers.map((header) => <th key={header.id}>{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</th>)}</tr>)}</thead><tbody>{table.getRowModel().rows.map((row) => <ResultRows key={row.id} row={row} expanded={expanded.has(row.original.id)} columnCount={columns.length} />)}</tbody></table></div>}
        <div className="flex flex-col items-center justify-between gap-3 border-t border-border px-4 py-3 text-xs text-muted-foreground sm:flex-row">
          <span>{resultsQuery.data?.totalItems ?? 0} results · page {resultsQuery.data?.page ?? page} of {Math.max(resultsQuery.data?.totalPages ?? 1, 1)}</span>
          <div className="flex items-center gap-2"><NativeSelect className="h-8 w-24" value={pageSize} onChange={(event) => { setPageSize(Number(event.target.value)); setPage(1) }} aria-label="Results per page"><option value={10}>10 / page</option><option value={25}>25 / page</option><option value={50}>50 / page</option><option value={100}>100 / page</option></NativeSelect><Button size="icon" variant="outline" disabled={page <= 1 || resultsQuery.isFetching} onClick={() => setPage((current) => current - 1)} aria-label="Previous page"><ChevronLeft className="h-4 w-4" /></Button><Button size="icon" variant="outline" disabled={page >= (resultsQuery.data?.totalPages ?? 1) || resultsQuery.isFetching} onClick={() => setPage((current) => current + 1)} aria-label="Next page"><ChevronRight className="h-4 w-4" /></Button>{resultsQuery.isFetching ? <Spinner /> : null}</div>
        </div>
      </Card>
      <span className="sr-only">{querySignature}</span>
      <ConfirmDialog open={confirmBatch} onOpenChange={setConfirmBatch} title="Delete selected results?" description={`${selected.size} persisted result records and their diagnostics will be permanently deleted. Task definitions are not affected.`} confirmLabel="Delete results" destructive busy={deleteMutation.isPending} onConfirm={() => deleteMutation.mutate([...selected])} />
    </>
  )
}

function SortButton({ label, column, sort, direction, onSort }: { label: string; column: string; sort: string; direction: 'asc' | 'desc'; onSort: (column: string) => void }) {
  return <button type="button" onClick={() => onSort(column)} className="inline-flex items-center gap-1 hover:text-foreground">{label}{sort === column ? direction === 'asc' ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" /> : null}</button>
}

function ResultRows({ row, expanded, columnCount }: { row: Row<typeof resultTableFeatures, Result>; expanded: boolean; columnCount: number }) {
  const result = row.original
  return <><tr>{row.getVisibleCells().map((cell) => <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}</tr>{expanded ? <tr><td colSpan={columnCount} className="!bg-muted/25 !p-4"><div className="grid gap-4 lg:grid-cols-[1fr_1fr_auto]"><div><p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Route & execution</p><dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-2 text-xs"><dt className="text-muted-foreground">Public IP</dt><dd className="font-mono">{result.detectedPublicIp || '—'}</dd><dt className="text-muted-foreground">Jitter / loss</dt><dd>{formatMilliseconds(result.jitterMilliseconds)} / {formatPercent(result.packetLossPercent)}</dd><dt className="text-muted-foreground">Duration</dt><dd>{formatMilliseconds(result.executionDurationMs)}</dd><dt className="text-muted-foreground">Provider version</dt><dd>{result.providerVersion || '—'}</dd><dt className="text-muted-foreground">TLS verification</dt><dd className={result.tlsVerificationDisabled ? 'font-semibold text-rose-600 dark:text-rose-400' : ''}>{result.tlsVerificationDisabled ? 'Disabled' : 'Enabled'}</dd></dl></div><div><p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Diagnostic message</p><p className={`mt-2 text-xs leading-5 ${result.sanitizedError ? 'text-rose-600 dark:text-rose-400' : 'text-muted-foreground'}`}>{result.sanitizedError || 'No provider or route error was recorded.'}</p></div><div><Button size="sm" asChild><Link to={`/results/${result.id}`}>Full diagnostics<ExternalLink className="h-3.5 w-3.5" /></Link></Button></div></div></td></tr> : null}</>
}
