import { useQuery } from '@tanstack/react-query'
import { Activity, Box, CheckCircle2, Clock3, Code2, Cpu, Database, HardDrive, Network, RefreshCw, Server, ShieldCheck, XCircle } from 'lucide-react'
import { api } from '../lib/api'
import { queryKeys } from '../lib/query'
import { formatBytes } from '../lib/utils'
import { PageHeader, SectionHeader } from '../components/common/PageHeader'
import { MetricCard } from '../components/common/MetricCard'
import { Badge, StatusDot } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { ErrorState, LoadingState, Spinner } from '../components/ui/states'

export default function SystemPage() {
  const systemQuery = useQuery({ queryKey: queryKeys.system, queryFn: api.system, refetchInterval: 60_000 })
  const healthQuery = useQuery({ queryKey: ['health'], queryFn: api.health, refetchInterval: 30_000 })
  if (systemQuery.isLoading) return <LoadingState label="Inspecting runtime health and versions…" />
  if (systemQuery.error || !systemQuery.data) return <ErrorState error={systemQuery.error ?? new Error('System information is unavailable.')} onRetry={() => void systemQuery.refetch()} />
  const info = systemQuery.data
  const providers = Array.isArray(info.providers) ? info.providers : []
  const interfaces = Array.isArray(info.interfaces) ? info.interfaces : []
  return (
    <>
      <PageHeader title="Runtime facts, without secrets." description="Build identity, database health, provider availability, interface state, and application uptime." actions={<Button variant="outline" onClick={() => { void systemQuery.refetch(); void healthQuery.refetch() }} disabled={systemQuery.isFetching}>{systemQuery.isFetching ? <Spinner /> : <RefreshCw className="h-4 w-4" />}Refresh</Button>} />
      <div className="mb-5 flex items-center gap-3 rounded-xl border border-emerald-500/25 bg-emerald-500/[.055] p-4"><span className="grid h-9 w-9 place-items-center rounded-lg bg-emerald-500/10 text-emerald-500"><ShieldCheck className="h-4 w-4" /></span><div className="min-w-0 flex-1"><p className="text-sm font-semibold">{healthQuery.isError ? 'Health check unavailable' : `Application ${healthQuery.data?.status ?? 'healthy'}`}</p><p className="text-xs text-muted-foreground">Sensitive environment variables and arbitrary filesystem content are never exposed here.</p></div><Badge tone={healthQuery.isError ? 'danger' : 'success'} className="gap-1"><StatusDot active={!healthQuery.isError} />{healthQuery.isError ? 'Degraded' : 'Healthy'}</Badge></div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="Uptime" value={formatUptime(info.uptimeSeconds)} detail={`MultiSpeed ${info.version || 'development'}`} icon={Clock3} tone="emerald" />
        <MetricCard label="Database" value={formatBytes(info.databaseSizeBytes)} detail={`Schema ${String(info.schemaVersion ?? 'unknown')}`} icon={Database} tone="cyan" />
        <MetricCard label="Persisted tasks" value={String(info.taskCount ?? 0)} detail={`${info.runningTaskCount ?? 0} currently active`} icon={Activity} tone="violet" />
        <MetricCard label="Results" value={String(info.resultCount ?? 0)} detail={`${interfaces.filter((item) => item.operational).length} interfaces up`} icon={HardDrive} tone="orange" />
      </div>
      <div className="mt-5 grid gap-5 xl:grid-cols-2">
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Box className="h-4 w-4 text-primary" />Build & runtime</CardTitle></CardHeader><CardContent><InfoList items={[
          ['MultiSpeed version', info.version], ['Git commit', info.gitCommit], ['Build date', info.buildDate || info.buildTime], ['Go version', info.goVersion], ['Operating system', info.operatingSystem], ['Architecture', info.architecture],
        ]} /></CardContent></Card>
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Database className="h-4 w-4 text-primary" />Persistence</CardTitle></CardHeader><CardContent><InfoList items={[
          ['Database path', info.databasePath], ['Database size', formatBytes(info.databaseSizeBytes)], ['Schema version', String(info.schemaVersion ?? '—')], ['Task count', String(info.taskCount ?? 0)], ['Result count', String(info.resultCount ?? 0)], ['Running tasks', String(info.runningTaskCount ?? 0)],
        ]} /></CardContent></Card>
      </div>
      <section className="mt-7"><SectionHeader title="Provider runtime" description="Executables and native adapter availability" /><div className="grid gap-4 md:grid-cols-3">{providers.map((provider) => <Card key={provider.id} className="p-4"><div className="flex items-start justify-between"><span className={`grid h-9 w-9 place-items-center rounded-lg ${provider.available ? 'bg-emerald-500/10 text-emerald-500' : 'bg-rose-500/10 text-rose-500'}`}>{provider.available ? <CheckCircle2 className="h-4 w-4" /> : <XCircle className="h-4 w-4" />}</span><Badge tone={provider.available ? 'success' : 'danger'}>{provider.available ? 'Available' : 'Unavailable'}</Badge></div><h3 className="mt-3 text-sm font-bold">{provider.displayName}</h3><p className="mt-1 font-mono text-[10px] text-muted-foreground">{provider.version || 'Version unavailable'}</p>{provider.message ? <p className="mt-3 text-xs leading-5 text-muted-foreground">{provider.message}</p> : null}</Card>)}{providers.length === 0 ? <Card className="p-4 text-xs text-muted-foreground md:col-span-3">Provider runtime details were not included by this build.</Card> : null}</div></section>
      <section className="mt-7"><SectionHeader title="Interface snapshot" description={`${interfaces.length} detected in the active Linux network namespace`} /><Card className="overflow-hidden"><div className="overflow-x-auto"><table className="data-table w-full min-w-[760px]"><thead><tr><th>Interface</th><th>State</th><th>MAC address</th><th>MTU</th><th>Addresses</th><th>Kind</th></tr></thead><tbody>{interfaces.map((item) => <tr key={item.name}><td className="font-semibold"><span className="flex items-center gap-2"><Network className="h-3.5 w-3.5 text-primary" />{item.name}</span></td><td><Badge tone={item.operational ? 'success' : 'neutral'} className="gap-1"><StatusDot active={item.operational} />{item.operational ? 'Up' : 'Down'}</Badge></td><td className="font-mono text-xs">{item.macAddress || '—'}</td><td>{item.mtu}</td><td className="max-w-md font-mono text-[11px]">{(Array.isArray(item.addresses) ? item.addresses : []).map((address) => address.address).join(' · ') || '—'}</td><td>{item.loopback ? 'Loopback' : item.virtual ? 'Virtual' : 'Physical'}</td></tr>)}</tbody></table></div></Card></section>
      <Card className="mt-7"><CardHeader><CardTitle className="flex items-center gap-2"><Code2 className="h-4 w-4 text-primary" />Runtime identifiers</CardTitle></CardHeader><CardContent><div className="grid gap-3 sm:grid-cols-3"><RuntimeFact icon={Cpu} label="Platform" value={`${info.operatingSystem || 'unknown'}/${info.architecture || 'unknown'}`} /><RuntimeFact icon={Server} label="Go runtime" value={info.goVersion || 'Unknown'} /><RuntimeFact icon={Database} label="Schema" value={String(info.schemaVersion ?? 'Unknown')} /></div></CardContent></Card>
    </>
  )
}

function InfoList({ items }: { items: Array<[string, string | number | undefined]> }) { return <dl className="divide-y divide-border">{items.map(([label, value]) => <div key={label} className="grid gap-1 py-2.5 first:pt-0 last:pb-0 sm:grid-cols-[145px_1fr]"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="break-all font-mono text-xs font-medium text-foreground">{value || '—'}</dd></div>)}</dl> }
function RuntimeFact({ icon: Icon, label, value }: { icon: typeof Cpu; label: string; value: string }) { return <div className="flex items-center gap-3 rounded-lg border border-border bg-background p-3"><span className="grid h-8 w-8 place-items-center rounded-lg bg-primary/10 text-primary"><Icon className="h-4 w-4" /></span><div><p className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</p><p className="mt-0.5 font-mono text-xs font-semibold">{value}</p></div></div> }
function formatUptime(seconds: number): string { if (!Number.isFinite(seconds)) return '—'; const days = Math.floor(seconds / 86400); const hours = Math.floor(seconds % 86400 / 3600); const minutes = Math.floor(seconds % 3600 / 60); return days ? `${days}d ${hours}h` : hours ? `${hours}h ${minutes}m` : `${minutes}m` }
