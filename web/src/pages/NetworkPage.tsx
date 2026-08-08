import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Cable, CheckCircle2, Clipboard, Edit3, Eye, Network, Plus, RefreshCw, Route, ShieldAlert, Trash2, XCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { z } from 'zod'
import { api, getApiErrorMessage } from '../lib/api'
import { queryKeys } from '../lib/query'
import type { NetworkInterface, RouteProfile, RouteProfileInput } from '../lib/types'
import { cn, copyText, formatMilliseconds } from '../lib/utils'
import { useFormatters } from '../hooks/useAppSettings'
import { PageHeader, SectionHeader } from '../components/common/PageHeader'
import { Badge, StatusDot } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { ConfirmDialog, Dialog, DialogContent } from '../components/ui/dialog'
import { CheckField, Field, Input, NativeSelect, Textarea } from '../components/ui/fields'
import { EmptyState, ErrorState, LoadingState, Spinner } from '../components/ui/states'
import { useToast } from '../components/ui/toast'

const routeSchema = z.object({
  name: z.string().trim().min(2, 'Enter at least 2 characters.').max(100),
  description: z.string().trim().max(500),
  interfaceName: z.string().min(1, 'Select an interface.'),
  sourceIp: z.string().min(1, 'Select a concrete source address.'),
  expectedGateway: z.string().trim().max(128),
  expectedRoutingTable: z.string().trim().max(64).refine((value) => /^(?:|[0-9]{1,10}|[A-Za-z][A-Za-z0-9_.-]{0,63})$/.test(value), 'Use a numeric table ID or a safe Linux table name.'),
  validationTarget: z.string().trim().min(1, 'Enter a hostname or IP to validate.').max(255),
  notes: z.string().trim().max(1000),
})
type RouteValues = z.infer<typeof routeSchema>
const routeDefaults: RouteValues = { name: '', description: '', interfaceName: '', sourceIp: '', expectedGateway: '', expectedRoutingTable: '', validationTarget: 'one.one.one.one', notes: '' }

export default function NetworkPage() {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [includeDown, setIncludeDown] = useState(false)
  const [includeVirtual, setIncludeVirtual] = useState(false)
  const [editRoute, setEditRoute] = useState<RouteProfile | 'new' | null>(null)
  const [deleteRoute, setDeleteRoute] = useState<RouteProfile | null>(null)
  const [validation, setValidation] = useState<{ profile: RouteProfile; pending: boolean } | null>(null)
  const interfacesQuery = useQuery({ queryKey: [...queryKeys.interfaces, includeDown, includeVirtual], queryFn: () => api.interfaces({ includeDown, includeVirtual }) })
  const routesQuery = useQuery({ queryKey: queryKeys.routes, queryFn: api.routes })
  const refreshMutation = useMutation({ mutationFn: api.refreshInterfaces, onSuccess: () => { void queryClient.invalidateQueries({ queryKey: queryKeys.interfaces }); void queryClient.invalidateQueries({ queryKey: queryKeys.tasks }); toast({ tone: 'success', title: 'Interface snapshot refreshed', description: 'Task bindings were re-evaluated against the active namespace.' }) }, onError: (error) => toast({ tone: 'error', title: 'Refresh failed', description: getApiErrorMessage(error) }) })
  const validateMutation = useMutation({ mutationFn: api.validateRoute, onSuccess: (result) => { setValidation((current) => current ? { ...current, pending: false } : null); void queryClient.invalidateQueries({ queryKey: queryKeys.routes }); toast({ tone: result.success ? 'success' : 'error', title: result.success ? 'Route matches expectations' : 'Route mismatch detected', description: result.message }) }, onError: (error) => { setValidation((current) => current ? { ...current, pending: false } : null); toast({ tone: 'error', title: 'Validation failed', description: getApiErrorMessage(error) }) } })
  const deleteMutation = useMutation({ mutationFn: api.deleteRoute, onSuccess: () => { setDeleteRoute(null); void queryClient.invalidateQueries({ queryKey: queryKeys.routes }); toast({ tone: 'success', title: 'Route profile deleted' }) }, onError: (error) => toast({ tone: 'error', title: 'Unable to delete profile', description: getApiErrorMessage(error) }) })
  const firstError = interfacesQuery.error ?? routesQuery.error
  if (interfacesQuery.isLoading || routesQuery.isLoading) return <LoadingState label="Inspecting Linux interfaces and route expectations…" />
  if (firstError) return <ErrorState error={firstError} onRetry={() => { void interfacesQuery.refetch(); void routesQuery.refetch() }} />
  const interfaces = interfacesQuery.data ?? []
  const routes = routesQuery.data ?? []
  const validate = (profile: RouteProfile) => { setValidation({ profile, pending: true }); validateMutation.mutate(profile.id) }
  return (
    <>
      <PageHeader title="Observe routes. Never rewrite them." description="Discover source addresses in the active Linux namespace and persist read-only expectations for source-based policy routing." actions={<Button variant="outline" onClick={() => refreshMutation.mutate()} disabled={refreshMutation.isPending}>{refreshMutation.isPending ? <Spinner /> : <RefreshCw className="h-4 w-4" />}Refresh interfaces</Button>} />
      <div className="mb-5 flex gap-3 rounded-xl border border-amber-500/25 bg-amber-500/[.055] p-4"><ShieldAlert className="mt-0.5 h-5 w-5 shrink-0 text-amber-500" /><div><p className="text-sm font-semibold">MultiSpeed does not modify host networking</p><p className="mt-1 text-xs leading-5 text-muted-foreground">Route profiles only validate route lookup, effective source, gateway, reachability, and public IP. Configure policy rules and gateways on the Docker host.</p></div></div>

      <section>
        <SectionHeader title="Detected interfaces" description={`${interfaces.length} interface${interfaces.length === 1 ? '' : 's'} shown from the active namespace`} action={<div className="flex gap-2"><CheckField checked={includeDown} onChange={setIncludeDown} label="Down" /><CheckField checked={includeVirtual} onChange={setIncludeVirtual} label="Virtual" /></div>} />
        {interfaces.length === 0 ? <Card><EmptyState title="No matching interfaces" description="Show down or virtual interfaces, or refresh discovery." /></Card> : <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">{interfaces.map((item) => <InterfaceCard key={item.name} item={item} />)}</div>}
      </section>

      <section className="mt-8">
        <SectionHeader title="Route profiles" description="Persisted expected paths used before speed tests" action={<Button size="sm" onClick={() => setEditRoute('new')}><Plus className="h-3.5 w-3.5" />New route profile</Button>} />
        {routes.length === 0 ? <Card><EmptyState title="No route expectations yet" description="Create a read-only profile to verify interface, source, gateway, routing table, reachability, and public IP." action={<Button size="sm" onClick={() => setEditRoute('new')}><Plus className="h-3.5 w-3.5" />Create profile</Button>} /></Card> : <div className="grid gap-4 lg:grid-cols-2">{routes.map((profile) => <RouteCard key={profile.id} profile={profile} onEdit={() => setEditRoute(profile)} onDelete={() => setDeleteRoute(profile)} onValidate={() => validate(profile)} validating={validation?.profile.id === profile.id && validation.pending} />)}</div>}
      </section>

      <PolicyExamples routes={routes} />
      <RouteDialog open={editRoute !== null} profile={editRoute === 'new' ? null : editRoute} interfaces={interfaces} onOpenChange={(open) => !open && setEditRoute(null)} onSaved={() => { setEditRoute(null); void queryClient.invalidateQueries({ queryKey: queryKeys.routes }) }} />
      <ConfirmDialog open={deleteRoute !== null} onOpenChange={(open) => !open && setDeleteRoute(null)} title="Delete route profile?" description={<><strong className="font-semibold text-foreground">{deleteRoute?.name}</strong> will no longer be available for task validation. Tasks that reference it must be updated first.</>} confirmLabel="Delete profile" destructive busy={deleteMutation.isPending} onConfirm={() => deleteRoute && deleteMutation.mutate(deleteRoute.id)} />
    </>
  )
}

function InterfaceCard({ item }: { item: NetworkInterface }) {
  const routable = item.addresses.filter((address) => !address.linkLocal)
  return <Card className={cn('overflow-hidden', !item.operational && 'opacity-75')}><div className={cn('h-1', item.operational ? 'bg-emerald-500' : 'bg-slate-500')} /><div className="p-4"><div className="flex items-start justify-between gap-3"><div className="flex items-center gap-3"><span className={cn('grid h-9 w-9 place-items-center rounded-lg', item.operational ? 'bg-emerald-500/10 text-emerald-500' : 'bg-muted text-muted-foreground')}><Cable className="h-4 w-4" /></span><div><h3 className="text-sm font-bold">{item.name}</h3><p className="mt-0.5 font-mono text-[10px] text-muted-foreground">{item.macAddress || 'MAC unavailable'}</p></div></div><Badge tone={item.operational ? 'success' : 'neutral'} className="gap-1"><StatusDot active={item.operational} />{item.operationalState || (item.operational ? 'Up' : 'Down')}</Badge></div><div className="mt-4 flex flex-wrap gap-1.5"><Badge tone="neutral">MTU {item.mtu}</Badge>{item.virtual ? <Badge tone="violet">Virtual</Badge> : null}{item.loopback ? <Badge tone="warning">Loopback</Badge> : null}</div><div className="mt-4 space-y-2">{routable.length ? routable.map((address) => <div key={address.address} className="flex items-center justify-between gap-2 rounded-md border border-border bg-background px-2.5 py-2"><code className="min-w-0 truncate text-[11px]">{address.address}</code><Badge tone={address.family === 'ipv4' ? 'info' : 'violet'}>{address.family.toUpperCase()}</Badge></div>) : <p className="text-xs text-muted-foreground">No routable source addresses detected.</p>}</div></div></Card>
}

function RouteCard({ profile, onEdit, onDelete, onValidate, validating }: { profile: RouteProfile; onEdit: () => void; onDelete: () => void; onValidate: () => void; validating: boolean }) {
  const { dateTime } = useFormatters()
  const snapshot = profile.lastValidationSnapshot
  const success = profile.lastValidationSucceeded
  return <Card><CardHeader className="flex-row items-start justify-between gap-3"><div className="flex items-start gap-3"><span className={cn('grid h-9 w-9 shrink-0 place-items-center rounded-lg', success === true ? 'bg-emerald-500/10 text-emerald-500' : success === false ? 'bg-rose-500/10 text-rose-500' : 'bg-muted text-muted-foreground')}><Route className="h-4 w-4" /></span><div><CardTitle>{profile.name}</CardTitle><p className="mt-1 text-xs text-muted-foreground">{profile.description || 'No description'}</p></div></div><Badge tone={success === true ? 'success' : success === false ? 'danger' : 'neutral'} className="gap-1">{success === true ? <CheckCircle2 className="h-3 w-3" /> : success === false ? <XCircle className="h-3 w-3" /> : <Eye className="h-3 w-3" />}{success === true ? 'Valid' : success === false ? 'Mismatch' : 'Not validated'}</Badge></CardHeader><CardContent><dl className="grid grid-cols-[125px_1fr] gap-x-3 gap-y-2 text-xs"><dt className="text-muted-foreground">Expected path</dt><dd className="font-medium">{profile.interfaceName} · <span className="font-mono">{profile.sourceIp}</span></dd><dt className="text-muted-foreground">Expected gateway</dt><dd>{profile.expectedGateway || 'Any'}</dd><dt className="text-muted-foreground">Detected gateway</dt><dd>{typeof snapshot.gateway === 'string' && snapshot.gateway ? snapshot.gateway : '—'}</dd><dt className="text-muted-foreground">Expected table</dt><dd>{profile.expectedRoutingTable || 'Any'}</dd><dt className="text-muted-foreground">Detected table</dt><dd>{typeof snapshot.routingTable === 'string' && snapshot.routingTable ? snapshot.routingTable : '—'}</dd><dt className="text-muted-foreground">Target</dt><dd>{profile.validationTarget}</dd>{profile.lastValidationAt ? <><dt className="text-muted-foreground">Last validation</dt><dd>{dateTime(profile.lastValidationAt)} · {formatMilliseconds(typeof snapshot.durationMs === 'number' ? snapshot.durationMs : null)}</dd><dt className="text-muted-foreground">Detected public IP</dt><dd className="font-mono">{typeof snapshot.detectedPublicIp === 'string' ? snapshot.detectedPublicIp : '—'}</dd></> : null}</dl>{success === false && typeof snapshot.message === 'string' ? <p className="mt-4 rounded-lg border border-rose-500/20 bg-rose-500/[.055] p-3 text-xs leading-5 text-rose-600 dark:text-rose-400">{snapshot.message}</p> : null}<div className="mt-4 flex flex-wrap gap-2"><Button size="sm" onClick={onValidate} disabled={validating}>{validating ? <Spinner /> : <Eye className="h-3.5 w-3.5" />}Validate now</Button><Button size="sm" variant="outline" onClick={onEdit}><Edit3 className="h-3.5 w-3.5" />Edit</Button><Button size="sm" variant="ghost" onClick={onDelete} className="ml-auto text-rose-600 dark:text-rose-400"><Trash2 className="h-3.5 w-3.5" />Delete</Button></div></CardContent></Card>
}

function RouteDialog({ open, profile, interfaces, onOpenChange, onSaved }: { open: boolean; profile: RouteProfile | null; interfaces: NetworkInterface[]; onOpenChange: (open: boolean) => void; onSaved: () => void }) {
  const { toast } = useToast()
  const form = useForm<RouteValues>({ resolver: zodResolver(routeSchema), defaultValues: routeDefaults })
  const interfaceName = useWatch({ control: form.control, name: 'interfaceName' })
  useEffect(() => { form.reset(profile ? { name: profile.name, description: profile.description, interfaceName: profile.interfaceName, sourceIp: profile.sourceIp, expectedGateway: profile.expectedGateway, expectedRoutingTable: profile.expectedRoutingTable, validationTarget: profile.validationTarget, notes: profile.notes } : routeDefaults) }, [form, profile, open])
  const mutation = useMutation({ mutationFn: (input: RouteProfileInput) => profile ? api.updateRoute(profile.id, input) : api.createRoute(input), onSuccess: (saved) => { toast({ tone: 'success', title: profile ? 'Route profile updated' : 'Route profile created', description: `${saved.name} is ready for read-only validation.` }); onSaved() }, onError: (error) => toast({ tone: 'error', title: 'Unable to save route profile', description: getApiErrorMessage(error) }) })
  const addresses = interfaces.find((item) => item.name === interfaceName)?.addresses.filter((address) => !address.linkLocal) ?? []
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent className="max-w-2xl" title={profile ? 'Edit route profile' : 'Create route profile'} description="Persist expected routing facts. MultiSpeed never executes route changes."><form onSubmit={form.handleSubmit((values) => mutation.mutate(values))} className="space-y-4"><div className="grid gap-4 sm:grid-cols-2"><Field label="Profile name" required error={form.formState.errors.name?.message}><Input {...form.register('name')} placeholder="Fiber policy route" /></Field><Field label="Validation target" required error={form.formState.errors.validationTarget?.message}><Input {...form.register('validationTarget')} placeholder="one.one.one.one" /></Field></div><Field label="Description" error={form.formState.errors.description?.message}><Input {...form.register('description')} placeholder="Expected path for the primary fiber uplink" /></Field><div className="grid gap-4 sm:grid-cols-2"><Field label="Interface" required error={form.formState.errors.interfaceName?.message}><NativeSelect {...form.register('interfaceName')} onChange={(event) => { void form.register('interfaceName').onChange(event); form.setValue('sourceIp', '') }}><option value="">Select interface…</option>{interfaces.map((item) => <option key={item.name} value={item.name}>{item.name} · {item.operationalState || (item.operational ? 'up' : 'down')}</option>)}</NativeSelect></Field><Field label="Source address" required error={form.formState.errors.sourceIp?.message}><NativeSelect {...form.register('sourceIp')} disabled={!interfaceName}><option value="">Select concrete address…</option>{addresses.map((item) => <option key={item.address} value={item.address}>{item.address} · {item.family.toUpperCase()}</option>)}</NativeSelect></Field></div><div className="grid gap-4 sm:grid-cols-2"><Field label="Expected gateway" hint="Optional" error={form.formState.errors.expectedGateway?.message}><Input {...form.register('expectedGateway')} placeholder="192.0.2.1" /></Field><Field label="Expected routing table" hint="ID or name" error={form.formState.errors.expectedRoutingTable?.message}><Input {...form.register('expectedRoutingTable')} placeholder="100 or wan_fiber" /></Field></div><Field label="Operator notes" hint="Optional" error={form.formState.errors.notes?.message}><Textarea {...form.register('notes')} placeholder="Host policy-routing prerequisites and change record." /></Field><div className="flex justify-end gap-2 pt-2"><Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>Cancel</Button><Button type="submit" disabled={mutation.isPending}>{mutation.isPending ? <Spinner /> : null}{profile ? 'Save changes' : 'Create profile'}</Button></div></form></DialogContent></Dialog>
}

function PolicyExamples({ routes }: { routes: RouteProfile[] }) {
  const { toast } = useToast()
  const profile = routes.find((item) => item.expectedGateway) ?? routes[0]
  const source = profile?.sourceIp || '192.0.2.10'
  const gateway = profile?.expectedGateway || '192.0.2.1'
  const device = profile?.interfaceName || 'eth1'
  const table = profile?.expectedRoutingTable || '100'
  const commands = `ip rule add from ${source.includes(':') ? `${source}/128` : `${source}/32`} table ${shellQuote(table)}\nip route add default via ${gateway} dev ${shellQuote(device)} table ${shellQuote(table)}`
  return <section className="mt-8"><SectionHeader title="Host policy-routing example" description="Copy-only examples derived from the selected route profile; never executed by MultiSpeed" /><Card><CardContent className="pt-5"><div className="flex items-start gap-3"><Network className="mt-1 h-4 w-4 shrink-0 text-primary" /><div className="min-w-0 flex-1"><pre className="overflow-x-auto rounded-lg border border-border bg-slate-950 p-4 text-xs leading-6 text-slate-200 scrollbar-thin"><code>{commands}</code></pre><p className="mt-3 text-xs leading-5 text-muted-foreground">Review interface names, prefixes, gateways, and table IDs on the Docker host. These examples are informational and may not match your network design.</p></div><Button variant="outline" size="sm" onClick={() => void copyText(commands).then(() => toast({ tone: 'success', title: 'Example commands copied' }))}><Clipboard className="h-3.5 w-3.5" />Copy</Button></div></CardContent></Card></section>
}

function shellQuote(value: string): string { return `'${value.replaceAll("'", `'"'"'`)}'` }
