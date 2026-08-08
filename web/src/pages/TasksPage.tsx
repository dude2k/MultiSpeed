import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { columnVisibilityFeature, createColumnHelper, flexRender, tableFeatures, useTable } from '@tanstack/react-table'
import { AlertTriangle, Copy, Edit3, FilterX, Play, Plus, Search, Trash2 } from 'lucide-react'
import { Link, useNavigate } from 'react-router'
import { api, getApiErrorMessage } from '../lib/api'
import { queryKeys } from '../lib/query'
import { taskToInput } from '../lib/tasks'
import type { ProviderId, Task } from '../lib/types'
import { formatRelative, providerLabel } from '../lib/utils'
import { PageHeader } from '../components/common/PageHeader'
import { ProviderBadge } from '../components/common/EntityBadges'
import { ActionMenu, ActionMenuItem, ActionMenuSeparator } from '../components/ui/menu'
import { Badge, StatusDot } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'
import { ConfirmDialog } from '../components/ui/dialog'
import { Input, NativeSelect, Switch } from '../components/ui/fields'
import { EmptyState, ErrorState, LoadingState, Spinner } from '../components/ui/states'
import { useToast } from '../components/ui/toast'

const taskTableFeatures = tableFeatures({ columnVisibilityFeature })
const columnHelper = createColumnHelper<typeof taskTableFeatures, Task>()

export default function TasksPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [search, setSearch] = useState('')
  const [provider, setProvider] = useState('all')
  const [enabled, setEnabled] = useState('all')
  const [network, setNetwork] = useState('all')
  const [deleteTask, setDeleteTask] = useState<Task | null>(null)
  const tasksQuery = useQuery({ queryKey: queryKeys.tasks, queryFn: api.tasks })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: queryKeys.tasks })
  const updateMutation = useMutation({
    mutationFn: ({ task, nextEnabled }: { task: Task; nextEnabled: boolean }) => api.updateTask(task.id, { ...taskToInput(task), enabled: nextEnabled }),
    onSuccess: (task) => { void invalidate(); toast({ tone: 'success', title: task.enabled ? 'Task enabled' : 'Task disabled', description: `${task.name} was updated immediately.` }) },
    onError: (error) => toast({ tone: 'error', title: 'Task update failed', description: getApiErrorMessage(error) }),
  })
  const runMutation = useMutation({
    mutationFn: api.runTask,
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['results'] }); toast({ tone: 'success', title: 'Test queued', description: 'Route validation will run before the measurement starts.' }) },
    onError: (error) => toast({ tone: 'error', title: 'Unable to queue test', description: getApiErrorMessage(error) }),
  })
  const duplicateMutation = useMutation({
    mutationFn: api.duplicateTask,
    onSuccess: (task) => { void invalidate(); toast({ tone: 'success', title: 'Task duplicated', description: `${task.name} is ready to review.` }); void navigate(`/tasks/${task.id}/edit`) },
    onError: (error) => toast({ tone: 'error', title: 'Duplicate failed', description: getApiErrorMessage(error) }),
  })
  const deleteMutation = useMutation({
    mutationFn: api.deleteTask,
    onSuccess: () => { setDeleteTask(null); void invalidate(); toast({ tone: 'success', title: 'Task deleted', description: 'Its historical results were preserved.' }) },
    onError: (error) => toast({ tone: 'error', title: 'Delete failed', description: getApiErrorMessage(error) }),
  })

  const tasks = useMemo(() => tasksQuery.data ?? [], [tasksQuery.data])
  const networks = useMemo(() => [...new Set(tasks.map((task) => task.interfaceName))].sort(), [tasks])
  const filtered = useMemo(() => tasks.filter((task) => {
    const term = search.trim().toLowerCase()
    const matchesSearch = !term || `${task.name} ${task.description} ${task.interfaceName} ${task.sourceIp}`.toLowerCase().includes(term)
    return matchesSearch && (provider === 'all' || task.provider === provider) && (enabled === 'all' || String(task.enabled) === enabled) && (network === 'all' || task.interfaceName === network)
  }), [tasks, search, provider, enabled, network])

  const columns = useMemo(() => columnHelper.columns([
    columnHelper.accessor('name', {
      header: 'Task',
      cell: ({ row }) => <div className="min-w-52"><div className="flex items-center gap-2"><Link to={`/tasks/${row.original.id}/edit`} className="font-semibold text-foreground hover:text-primary hover:underline">{row.original.name}</Link>{row.original.networkPathValid === false ? <Badge tone="danger" className="gap-1"><AlertTriangle className="h-3 w-3" />Invalid path</Badge> : null}</div><p className="mt-0.5 max-w-xs truncate text-[11px] text-muted-foreground">{row.original.description || 'No description'}</p></div>,
    }),
    columnHelper.accessor('provider', { header: 'Provider', cell: ({ getValue }) => <ProviderBadge provider={getValue()} /> }),
    columnHelper.accessor('interfaceName', { header: 'Network path', cell: ({ row }) => <div><p className="font-medium text-foreground">{row.original.interfaceName}</p><p className="mt-0.5 font-mono text-[11px] text-muted-foreground">{row.original.sourceIp}</p>{row.original.networkPathValid === false ? <p className="mt-1 max-w-56 text-[10px] leading-4 text-rose-600 dark:text-rose-400">{row.original.networkPathMessage || 'The configured interface or source address is unavailable.'}</p> : null}</div> }),
    columnHelper.accessor('cronExpression', { header: 'Schedule', cell: ({ row }) => <div><p className="font-mono text-xs font-semibold text-foreground">{row.original.cronExpression}</p><p className="mt-0.5 text-[11px] text-muted-foreground">{row.original.timezone}</p></div> }),
    columnHelper.accessor('nextScheduledAt', { header: 'Next run', cell: ({ row }) => <div><p className="text-xs font-medium text-foreground">{row.original.enabled ? formatRelative(row.original.nextScheduledAt) : 'Paused'}</p><p className="mt-0.5 text-[11px] text-muted-foreground">{row.original.lastScheduledAt ? `Last ${formatRelative(row.original.lastScheduledAt)}` : 'Never run'}</p></div> }),
    columnHelper.accessor('enabled', { header: 'Enabled', cell: ({ row }) => <div className="flex items-center gap-2"><Switch checked={row.original.enabled} onCheckedChange={(nextEnabled) => { if (nextEnabled && !row.original.enabled) { toast({ tone: 'info', title: 'Preflight required before enabling', description: 'Review this task and validate its current provider, target, and source path.' }); void navigate(`/tasks/${row.original.id}/edit`); return } updateMutation.mutate({ task: row.original, nextEnabled }) }} aria-label={`${row.original.enabled ? 'Disable' : 'Enable'} ${row.original.name}`} /><Badge tone={row.original.enabled ? 'success' : 'neutral'} className="gap-1"><StatusDot active={row.original.enabled} />{row.original.enabled ? 'On' : 'Off'}</Badge></div> }),
    columnHelper.display({
      id: 'actions',
      header: '',
      cell: ({ row }) => <ActionMenu label={`Actions for ${row.original.name}`}>
        <ActionMenuItem onSelect={() => void navigate(`/tasks/${row.original.id}/edit`)}><Edit3 className="h-3.5 w-3.5" />Edit task</ActionMenuItem>
        <ActionMenuItem disabled={runMutation.isPending || row.original.networkPathValid === false} onSelect={() => runMutation.mutate(row.original.id)}><Play className="h-3.5 w-3.5" />Run now</ActionMenuItem>
        <ActionMenuItem disabled={duplicateMutation.isPending} onSelect={() => duplicateMutation.mutate(row.original.id)}><Copy className="h-3.5 w-3.5" />Duplicate</ActionMenuItem>
        <ActionMenuSeparator />
        <ActionMenuItem danger onSelect={() => setDeleteTask(row.original)}><Trash2 className="h-3.5 w-3.5" />Delete</ActionMenuItem>
      </ActionMenu>,
    }),
  ]), [duplicateMutation, navigate, runMutation, toast, updateMutation])
  const table = useTable({ features: taskTableFeatures, data: filtered, columns })

  if (tasksQuery.isLoading) return <LoadingState label="Loading independent schedules…" />
  if (tasksQuery.error) return <ErrorState error={tasksQuery.error} onRetry={() => void tasksQuery.refetch()} />

  const filtersActive = search || provider !== 'all' || enabled !== 'all' || network !== 'all'
  const resetFilters = () => { setSearch(''); setProvider('all'); setEnabled('all'); setNetwork('all') }

  return (
    <>
      <PageHeader title="Independent tests, precisely routed." description="Each task owns its provider, target, schedule, source address, and route-validation policy." actions={<Button asChild><Link to="/tasks/new"><Plus className="h-4 w-4" />Create task</Link></Button>} />
      <Card className="overflow-hidden">
        <div className="grid gap-3 border-b border-border p-4 md:grid-cols-[minmax(220px,1fr)_repeat(3,minmax(140px,.36fr))_auto]">
          <div className="relative"><Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" /><Input className="pl-9" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search name, WAN, or source IP…" aria-label="Search tasks" /></div>
          <NativeSelect value={provider} onChange={(event) => setProvider(event.target.value)} aria-label="Filter by provider"><option value="all">All providers</option>{(['ookla', 'librespeed', 'cloudflare'] as ProviderId[]).map((item) => <option key={item} value={item}>{providerLabel(item)}</option>)}</NativeSelect>
          <NativeSelect value={enabled} onChange={(event) => setEnabled(event.target.value)} aria-label="Filter by enabled state"><option value="all">Any state</option><option value="true">Enabled</option><option value="false">Disabled</option></NativeSelect>
          <NativeSelect value={network} onChange={(event) => setNetwork(event.target.value)} aria-label="Filter by network interface"><option value="all">All interfaces</option>{networks.map((item) => <option key={item} value={item}>{item}</option>)}</NativeSelect>
          <Button variant="ghost" size="icon" onClick={resetFilters} disabled={!filtersActive} aria-label="Clear filters"><FilterX className="h-4 w-4" /></Button>
        </div>

        {filtered.length === 0 ? <EmptyState title={tasks.length ? 'No tasks match these filters' : 'Create your first measurement path'} description={tasks.length ? 'Clear or adjust filters to see more tasks.' : 'Configure a provider, concrete source address, and independent schedule.'} action={tasks.length ? <Button size="sm" variant="outline" onClick={resetFilters}><FilterX className="h-3.5 w-3.5" />Clear filters</Button> : <Button asChild size="sm"><Link to="/tasks/new"><Plus className="h-3.5 w-3.5" />Create task</Link></Button>} /> : <div className="overflow-x-auto scrollbar-thin"><table className="data-table w-full min-w-[1000px]"><thead>{table.getHeaderGroups().map((group) => <tr key={group.id}>{group.headers.map((header) => <th key={header.id}>{header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}</th>)}</tr>)}</thead><tbody>{table.getRowModel().rows.map((row) => <tr key={row.id}>{row.getVisibleCells().map((cell) => <td key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>)}</tr>)}</tbody></table></div>}
        <div className="flex items-center justify-between border-t border-border px-4 py-3 text-xs text-muted-foreground"><span>{filtered.length} of {tasks.length} task{tasks.length === 1 ? '' : 's'}</span>{updateMutation.isPending || runMutation.isPending || duplicateMutation.isPending ? <span className="flex items-center gap-1.5"><Spinner />Applying change…</span> : <span>Schedules update immediately</span>}</div>
      </Card>

      <ConfirmDialog open={deleteTask !== null} onOpenChange={(open) => !open && setDeleteTask(null)} title="Delete this task?" description={<><strong className="font-semibold text-foreground">{deleteTask?.name}</strong> and its future schedule will be removed. Existing results remain available for analysis.</>} confirmLabel="Delete task" destructive busy={deleteMutation.isPending} onConfirm={() => deleteTask && deleteMutation.mutate(deleteTask.id)} />
    </>
  )
}
