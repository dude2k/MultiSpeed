import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowLeft, ArrowUp, Clipboard, Clock3, ExternalLink, Gauge, Network, RadioTower, Route, Trash2 } from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router'
import { api, getApiErrorMessage } from '../lib/api'
import { queryKeys } from '../lib/query'
import { copyText, formatBytes, formatMilliseconds, formatPercent, safeExternalHttpUrl } from '../lib/utils'
import { useFormatters } from '../hooks/useAppSettings'
import { PageHeader } from '../components/common/PageHeader'
import { MetricCard } from '../components/common/MetricCard'
import { ProviderBadge, ResultStatusBadge } from '../components/common/EntityBadges'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { ConfirmDialog } from '../components/ui/dialog'
import { ErrorState, LoadingState } from '../components/ui/states'
import { useToast } from '../components/ui/toast'

export default function ResultDetailPage() {
  const { resultId = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const { bitrate, dateTime } = useFormatters()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const resultQuery = useQuery({ queryKey: queryKeys.result(resultId), queryFn: () => api.result(resultId) })
  const tasksQuery = useQuery({ queryKey: queryKeys.tasks, queryFn: api.tasks })
  const deleteMutation = useMutation({ mutationFn: () => api.deleteResult(resultId), onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['results'] }); toast({ tone: 'success', title: 'Result deleted' }); void navigate('/results') }, onError: (error) => toast({ tone: 'error', title: 'Delete failed', description: getApiErrorMessage(error) }) })
  if (resultQuery.isLoading || tasksQuery.isLoading) return <LoadingState label="Loading full result diagnostics…" />
  const firstError = resultQuery.error ?? tasksQuery.error
  if (firstError || !resultQuery.data) return <ErrorState error={firstError ?? new Error('Result not found.')} onRetry={() => void resultQuery.refetch()} />
  const result = resultQuery.data
  const task = tasksQuery.data?.find((item) => item.id === result.taskId)
  const route = result.routeValidationSnapshot ?? {}
  const providerResultUrl = safeExternalHttpUrl(result.providerResultUrl)
  const copy = (value: string, label: string) => void copyText(value).then(() => toast({ tone: 'success', title: `${label} copied` })).catch((error: unknown) => toast({ tone: 'error', title: 'Copy failed', description: getApiErrorMessage(error) }))
  return (
    <>
      <PageHeader title={task?.name ?? 'Measurement diagnostics'} description={`${dateTime(result.startedAt, true)} · ${result.trigger} execution · result ${result.id}`} actions={<><Button asChild variant="outline"><Link to="/results"><ArrowLeft className="h-4 w-4" />Results</Link></Button><Button variant="danger" onClick={() => setConfirmDelete(true)}><Trash2 className="h-4 w-4" />Delete</Button></>} />
      <div className="mb-5 flex flex-wrap items-center gap-2"><ResultStatusBadge status={result.status} /><ProviderBadge provider={result.provider} />{task ? <Button asChild variant="ghost" size="sm"><Link to={`/tasks/${task.id}/edit`}>Open task<ExternalLink className="h-3.5 w-3.5" /></Link></Button> : null}{providerResultUrl ? <Button asChild variant="ghost" size="sm"><a href={providerResultUrl} target="_blank" rel="noreferrer">Provider result<ExternalLink className="h-3.5 w-3.5" /></a></Button> : null}</div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="Download" value={bitrate(result.downloadBitsPerSecond)} detail={formatBytes(result.downloadBytes)} icon={ArrowDown} tone="cyan" />
        <MetricCard label="Upload" value={bitrate(result.uploadBitsPerSecond)} detail={formatBytes(result.uploadBytes)} icon={ArrowUp} tone="violet" />
        <MetricCard label="Latency" value={formatMilliseconds(result.latencyMilliseconds)} detail={`Jitter ${formatMilliseconds(result.jitterMilliseconds)}`} icon={Gauge} tone="orange" />
        <MetricCard label="Packet loss" value={formatPercent(result.packetLossPercent)} detail={`Duration ${formatMilliseconds(result.executionDurationMs)}`} icon={RadioTower} tone={result.packetLossPercent && result.packetLossPercent > 1 ? 'rose' : 'emerald'} />
      </div>
      {result.sanitizedError ? <Card className="mt-5 border-rose-500/25 bg-rose-500/[.055] p-4"><p className="text-xs font-semibold uppercase tracking-wider text-rose-600 dark:text-rose-400">Failure explanation</p><p className="mt-2 whitespace-pre-wrap text-sm leading-6 text-foreground">{result.sanitizedError}</p></Card> : null}
      {result.tlsVerificationDisabled ? <Card className="mt-5 border-rose-500/25 bg-rose-500/[.055] p-4"><p className="text-xs font-semibold uppercase tracking-wider text-rose-600 dark:text-rose-400">TLS verification was disabled</p><p className="mt-2 text-sm leading-6 text-foreground">This measurement trusted an unverified LibreSpeed endpoint certificate. Treat its throughput, latency, and public-IP metadata as security-sensitive diagnostics.</p></Card> : null}
      <div className="mt-5 grid gap-5 xl:grid-cols-2">
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Network className="h-4 w-4 text-primary" />Network path</CardTitle></CardHeader><CardContent><DetailList items={[
          ['Selected interface', result.selectedInterface], ['Selected source IP', result.selectedSourceIp], ['Detected public IP', result.detectedPublicIp], ['IP route profile', result.routeProfileId ?? 'None'], ['Cloudflare colo', result.cloudflareColo || 'Not applicable'],
        ]} /></CardContent></Card>
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><RadioTower className="h-4 w-4 text-primary" />Provider target</CardTitle></CardHeader><CardContent><DetailList items={[
          ['Server ID', result.serverId], ['Server name', result.serverName], ['Server host', result.serverHost], ['Sponsor', result.serverSponsor], ['Location', [result.serverLocation, result.serverCountry].filter(Boolean).join(', ')], ['Provider version', result.providerVersion],
        ]} /></CardContent></Card>
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Route className="h-4 w-4 text-primary" />Route-validation snapshot</CardTitle></CardHeader><CardContent>{Object.keys(route).length ? <DetailList items={Object.entries(route).map(([key, value]) => [humanize(key), formatUnknown(value)])} /> : <p className="text-xs text-muted-foreground">No persisted route-validation snapshot was returned for this result.</p>}</CardContent></Card>
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Clock3 className="h-4 w-4 text-primary" />Execution metadata</CardTitle></CardHeader><CardContent><DetailList items={[
          ['Scheduled', dateTime(result.scheduledAt, true)], ['Started', dateTime(result.startedAt, true)], ['Finished', dateTime(result.finishedAt, true)], ['Duration', formatMilliseconds(result.executionDurationMs)], ['Process exit code', result.processExitCode == null ? 'Not applicable' : String(result.processExitCode)], ['TLS verification', result.tlsVerificationDisabled ? 'Disabled (unsafe)' : 'Enabled'], ['Application version', result.applicationVersion],
        ]} /></CardContent></Card>
      </div>
      <Card className="mt-5"><CardHeader className="flex-row items-center justify-between"><div><CardTitle>Sanitized provider response</CardTitle><p className="mt-1 text-xs text-muted-foreground">Size-limited diagnostic payload stored with the result</p></div><Button variant="outline" size="sm" onClick={() => copy(result.rawProviderResponse || '{}', 'Raw provider response')}><Clipboard className="h-3.5 w-3.5" />Copy</Button></CardHeader><CardContent><pre className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-all rounded-lg border border-border bg-slate-950 p-4 text-xs leading-5 text-slate-200 scrollbar-thin">{prettyJson(result.rawProviderResponse || '{}')}</pre></CardContent></Card>
      <ConfirmDialog open={confirmDelete} onOpenChange={setConfirmDelete} title="Delete this result?" description="The measurement, route snapshot, and sanitized provider diagnostics will be permanently removed. The task remains unchanged." confirmLabel="Delete result" destructive busy={deleteMutation.isPending} onConfirm={() => deleteMutation.mutate()} />
    </>
  )
}

function DetailList({ items }: { items: Array<[string, string]> }) {
  return <dl className="divide-y divide-border">{items.map(([label, value]) => <div key={label} className="grid gap-1 py-2.5 first:pt-0 last:pb-0 sm:grid-cols-[150px_1fr]"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="break-all text-xs font-medium text-foreground">{value || '—'}</dd></div>)}</dl>
}

function humanize(value: string): string { return value.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/_/g, ' ').replace(/^./, (character) => character.toUpperCase()) }
function formatUnknown(value: unknown): string { if (value == null) return '—'; if (typeof value === 'string') return value; if (typeof value === 'number' || typeof value === 'boolean' || typeof value === 'bigint') return value.toString(); return JSON.stringify(value) }
function prettyJson(value: string): string { try { return JSON.stringify(JSON.parse(value) as unknown, null, 2) } catch { return value || '{}' } }
