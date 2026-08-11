import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, Database, Download, FileJson, Gauge, Network, Save, Scale, ShieldAlert, TimerReset, Trash2, Upload } from 'lucide-react'
import { type ChangeEvent, useEffect, useRef, useState } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { Link } from 'react-router'
import { z } from 'zod'
import { api, getApiErrorMessage } from '../lib/api'
import { ianaTimezones, isValidTimezone, validateCron } from '../lib/cron'
import { queryKeys } from '../lib/query'
import type { ConfigurationDocument, Settings } from '../lib/types'
import { PageHeader } from '../components/common/PageHeader'
import { OoklaEulaAcceptance } from '../components/common/OoklaEulaAcceptance'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { ConfirmDialog } from '../components/ui/dialog'
import { CheckField, Field, Input, NativeSelect } from '../components/ui/fields'
import { ErrorState, LoadingState, Spinner } from '../components/ui/states'
import { useToast } from '../components/ui/toast'
import { fallbackSettings } from '../hooks/useAppSettings'

const settingsSchema = z.object({
  displayUnits: z.enum(['bits', 'bytes']),
  defaultTimezone: z.string().min(1).refine(isValidTimezone, 'Select a valid IANA timezone.'),
  globalConcurrency: z.number().int().min(1).max(16),
  allowSeparateWanConcurrency: z.boolean(),
  retentionMode: z.enum(['forever', 'days', 'months']),
  retentionValue: z.number().int().min(0).max(3650),
  defaultChartRange: z.string().min(1),
  interfaceRefreshIntervalSeconds: z.number().int().min(5).max(3600),
  defaultTaskTimeoutSeconds: z.number().int().min(5).max(3600),
  databaseMaintenanceSchedule: z.string().refine(validateCron, 'Enter a valid five-field cron expression.'),
  ooklaEulaAccepted: z.boolean(),
  ooklaEulaAcceptedAt: z.string().nullable(),
  ooklaEulaVersion: z.string(),
  ooklaEulaCurrentVersion: z.string(),
  ooklaEulaEffectiveAccepted: z.boolean(),
  ooklaEulaAcceptanceSource: z.enum(['none', 'persisted', 'environment']),
}).superRefine((value, context) => { if (value.retentionMode !== 'forever' && value.retentionValue < 1) context.addIssue({ code: 'custom', path: ['retentionValue'], message: 'Enter a positive retention period.' }) })

const maximumConfigurationBytes = 1 << 20

export function parseConfigurationPreview(text: string): ConfigurationDocument {
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch {
    throw new Error('The selected file is not valid JSON.')
  }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('The selected file does not contain a configuration object.')
  const candidate = value as Record<string, unknown>
  if (candidate.format !== 'multispeed-config' || candidate.version !== 1) throw new Error('The selected file is not a supported MultiSpeed configuration export.')
  if (typeof candidate.exportedAt !== 'string' || typeof candidate.settings !== 'object' || candidate.settings === null || !Array.isArray(candidate.tasks) || !Array.isArray(candidate.routeProfiles)) {
    throw new Error('The selected configuration is incomplete.')
  }
  return value as ConfigurationDocument
}

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}

export default function SettingsPage() {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [confirmCleanup, setConfirmCleanup] = useState(false)
  const [pendingConfiguration, setPendingConfiguration] = useState<{ document: ConfigurationDocument; filename: string } | null>(null)
  const configurationInput = useRef<HTMLInputElement>(null)
  const settingsQuery = useQuery({ queryKey: queryKeys.settings, queryFn: api.settings })
  const form = useForm<Settings>({ resolver: zodResolver(settingsSchema), defaultValues: fallbackSettings, mode: 'onBlur' })
  useEffect(() => { if (settingsQuery.data) form.reset(settingsQuery.data) }, [form, settingsQuery.data])
  const saveMutation = useMutation({ mutationFn: api.updateSettings, onSuccess: (settings) => { form.reset(settings); void queryClient.invalidateQueries({ queryKey: queryKeys.settings }); toast({ tone: 'success', title: 'Settings saved', description: 'Scheduler and maintenance defaults were updated.' }) }, onError: (error) => toast({ tone: 'error', title: 'Unable to save settings', description: getApiErrorMessage(error) }) })
  const backupMutation = useMutation({ mutationFn: api.backup, onSuccess: (result) => { downloadBlob(result.blob, result.filename); toast({ tone: 'success', title: 'Consistent backup created', description: `${result.filename} includes committed WAL data.` }) }, onError: (error) => toast({ tone: 'error', title: 'Backup failed', description: getApiErrorMessage(error) }) })
  const configurationExportMutation = useMutation({ mutationFn: api.exportConfiguration, onSuccess: (result) => { downloadBlob(result.blob, result.filename); toast({ tone: 'success', title: 'Configuration exported', description: `${result.filename} contains settings, tasks, and route profiles.` }) }, onError: (error) => toast({ tone: 'error', title: 'Export failed', description: getApiErrorMessage(error) }) })
  const configurationImportMutation = useMutation({
    mutationFn: (document: ConfigurationDocument) => api.importConfiguration(document),
    onSuccess: async (result) => {
      setPendingConfiguration(null)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.settings }),
        queryClient.invalidateQueries({ queryKey: queryKeys.tasks }),
        queryClient.invalidateQueries({ queryKey: queryKeys.routes }),
        queryClient.invalidateQueries({ queryKey: queryKeys.system }),
      ])
      toast({ tone: 'success', title: 'Configuration imported', description: `${result.taskCount} tasks and ${result.routeProfileCount} route profiles restored. Results and the Ookla terms acknowledgement were preserved.` })
    },
    onError: (error) => toast({ tone: 'error', title: 'Import failed', description: getApiErrorMessage(error) }),
  })
  const cleanupMutation = useMutation({ mutationFn: () => api.cleanupResults(), onSuccess: (result) => { setConfirmCleanup(false); void queryClient.invalidateQueries({ queryKey: ['results'] }); void queryClient.invalidateQueries({ queryKey: ['statistics'] }); toast({ tone: 'success', title: 'Retention cleanup complete', description: `${result.deletedResults ?? result.deleted ?? 0} expired results removed in bounded batches.` }) }, onError: (error) => toast({ tone: 'error', title: 'Cleanup failed', description: getApiErrorMessage(error) }) })
  const { register, control, formState: { errors, isDirty } } = form
  const retentionMode = useWatch({ control, name: 'retentionMode' })
  const defaultTimezone = useWatch({ control, name: 'defaultTimezone' })
  const selectConfiguration = async (event: ChangeEvent<HTMLInputElement>) => {
    const input = event.currentTarget
    const file = input.files?.[0]
    if (!file) return
    try {
      if (file.size > maximumConfigurationBytes) throw new Error('Configuration files must not exceed 1 MiB.')
      setPendingConfiguration({ document: parseConfigurationPreview(await file.text()), filename: file.name })
    } catch (error) {
      toast({ tone: 'error', title: 'Configuration file rejected', description: getApiErrorMessage(error) })
    } finally {
      input.value = ''
    }
  }
  if (settingsQuery.isLoading) return <LoadingState label="Loading runtime defaults…" />
  if (settingsQuery.error) return <ErrorState error={settingsQuery.error} onRetry={() => void settingsQuery.refetch()} />
  return (
    <form onSubmit={form.handleSubmit((values) => saveMutation.mutate(values))}>
      <PageHeader title="Safe operational defaults." description="Tune new-task defaults, concurrency, retention, interface discovery, and database maintenance." actions={<Button type="submit" disabled={saveMutation.isPending || !isDirty}>{saveMutation.isPending ? <Spinner /> : <Save className="h-4 w-4" />}Save settings</Button>} />
      <div className="mb-5 flex gap-3 rounded-xl border border-rose-500/25 bg-rose-500/[.055] p-4"><ShieldAlert className="mt-0.5 h-5 w-5 shrink-0 text-rose-500" /><div><p className="text-sm font-semibold">Trusted network only</p><p className="mt-1 text-xs leading-5 text-muted-foreground">MultiSpeed does not include authentication and must only be exposed to trusted networks unless protected by an authenticating reverse proxy.</p></div></div>
      <div className="grid gap-5 xl:grid-cols-2">
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Gauge className="h-4 w-4 text-primary" />Display & task defaults</CardTitle><p className="text-xs text-muted-foreground">Applied to new tasks and reporting views</p></CardHeader><CardContent className="space-y-4"><div className="grid gap-4 sm:grid-cols-2"><Field label="Display units" error={errors.displayUnits?.message}><NativeSelect {...register('displayUnits')}><option value="bits">Bits per second</option><option value="bytes">Bytes per second</option></NativeSelect></Field><Field label="Default chart range" error={errors.defaultChartRange?.message}><NativeSelect {...register('defaultChartRange')}><option value="24h">Last 24 hours</option><option value="7d">Last 7 days</option><option value="30d">Last 30 days</option><option value="90d">Last 90 days</option></NativeSelect></Field></div><div className="grid gap-4 sm:grid-cols-2"><Field label="Default timezone" error={errors.defaultTimezone?.message}><NativeSelect {...register('defaultTimezone')}>{ianaTimezones.includes(defaultTimezone) ? null : <option value={defaultTimezone}>{defaultTimezone}</option>}{ianaTimezones.map((zone) => <option key={zone} value={zone}>{zone}</option>)}</NativeSelect></Field><Field label="Default task timeout" hint="Seconds" error={errors.defaultTaskTimeoutSeconds?.message}><Input type="number" min={5} max={3600} {...register('defaultTaskTimeoutSeconds', { valueAsNumber: true })} /></Field></div></CardContent></Card>
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Network className="h-4 w-4 text-primary" />Concurrency & discovery</CardTitle><p className="text-xs text-muted-foreground">The global concurrency default is one; same-WAN tests never overlap</p></CardHeader><CardContent className="space-y-4"><div className="grid gap-4 sm:grid-cols-2"><Field label="Global concurrency" hint="1–16" error={errors.globalConcurrency?.message}><Input type="number" min={1} max={16} {...register('globalConcurrency', { valueAsNumber: true })} /></Field><Field label="Interface refresh interval" hint="Seconds" error={errors.interfaceRefreshIntervalSeconds?.message}><Input type="number" min={5} max={3600} {...register('interfaceRefreshIntervalSeconds', { valueAsNumber: true })} /></Field></div><Controller name="allowSeparateWanConcurrency" control={control} render={({ field }) => <CheckField checked={field.value} onChange={field.onChange} label="Allow concurrency on separate WANs" description="Only tasks with distinct interface/source locks may execute together, bounded by global concurrency." />} /></CardContent></Card>
        <Card id="ookla-eula" className="xl:col-span-2"><CardHeader><CardTitle className="flex items-center gap-2"><Scale className="h-4 w-4 text-primary" />Ookla provider terms & authorization</CardTitle><p className="text-xs text-muted-foreground">Technical acknowledgement and CLI flag authorization, independent from installation and any separate license permission</p></CardHeader><CardContent><OoklaEulaAcceptance /></CardContent></Card>
        <Card className="xl:col-span-2">
          <CardHeader><CardTitle className="flex items-center gap-2"><FileJson className="h-4 w-4 text-primary" />Configuration import & export</CardTitle><p className="text-xs text-muted-foreground">Portable JSON for operational settings, tasks, and route profiles</p></CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-2">
            <div className="flex items-start justify-between gap-4 rounded-lg border border-border bg-muted/30 p-3">
              <div><p className="text-xs font-semibold">Export current configuration</p><p className="mt-1 text-[11px] leading-5 text-muted-foreground">Downloads a versioned JSON file. Results and the Ookla terms acknowledgement are excluded.</p></div>
              <Button type="button" size="sm" variant="outline" onClick={() => configurationExportMutation.mutate()} disabled={configurationExportMutation.isPending}>{configurationExportMutation.isPending ? <Spinner /> : <Download className="h-3.5 w-3.5" />}Export</Button>
            </div>
            <div className="flex items-start justify-between gap-4 rounded-lg border border-border bg-muted/30 p-3">
              <div><p className="text-xs font-semibold">Import configuration</p><p className="mt-1 text-[11px] leading-5 text-muted-foreground">Validates the complete file before atomically replacing saved configuration.</p></div>
              <input ref={configurationInput} className="sr-only" type="file" accept="application/json,.json" aria-label="Configuration file" onChange={(event) => void selectConfiguration(event)} />
              <Button type="button" size="sm" variant="outline" onClick={() => configurationInput.current?.click()} disabled={configurationImportMutation.isPending}><Upload className="h-3.5 w-3.5" />Choose file</Button>
            </div>
          </CardContent>
        </Card>
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Archive className="h-4 w-4 text-primary" />Retention</CardTitle><p className="text-xs text-muted-foreground">Cleanup deletes old results in bounded batches and preserves task definitions</p></CardHeader><CardContent className="space-y-4"><div className="grid gap-4 sm:grid-cols-2"><Field label="Retention policy" error={errors.retentionMode?.message}><NativeSelect {...register('retentionMode')}><option value="forever">Keep forever</option><option value="days">Fixed number of days</option><option value="months">Fixed number of months</option></NativeSelect></Field><Field label={retentionMode === 'months' ? 'Months to retain' : 'Days to retain'} error={errors.retentionValue?.message}><Input type="number" min={retentionMode === 'forever' ? 0 : 1} max={3650} disabled={retentionMode === 'forever'} {...register('retentionValue', { valueAsNumber: true })} /></Field></div><div className="flex items-start justify-between gap-4 rounded-lg border border-border bg-muted/30 p-3"><div><p className="text-xs font-semibold">Run configured cleanup now</p><p className="mt-1 text-[11px] leading-5 text-muted-foreground">Applies the saved retention policy in bounded batches. Review individual records separately.</p></div><div className="flex gap-2"><Button asChild size="sm" variant="ghost"><Link to="/results">Review</Link></Button><Button type="button" size="sm" variant="outline" onClick={() => setConfirmCleanup(true)} disabled={retentionMode === 'forever'}><Trash2 className="h-3.5 w-3.5" />Clean up</Button></div></div></CardContent></Card>
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Database className="h-4 w-4 text-primary" />Database maintenance</CardTitle><p className="text-xs text-muted-foreground">WAL checkpointing and bounded upkeep</p></CardHeader><CardContent className="space-y-4"><Field label="Maintenance schedule" hint="Five-field cron" error={errors.databaseMaintenanceSchedule?.message}><Input className="font-mono" {...register('databaseMaintenanceSchedule')} placeholder="30 3 * * *" /></Field><div className="flex items-start justify-between gap-4 rounded-lg border border-border bg-muted/30 p-3"><div><p className="text-xs font-semibold">Consistent SQLite backup</p><p className="mt-1 text-[11px] leading-5 text-muted-foreground">Creates a transactional backup that includes active WAL data.</p></div><Button type="button" size="sm" variant="outline" onClick={() => backupMutation.mutate()} disabled={backupMutation.isPending}>{backupMutation.isPending ? <Spinner /> : <Download className="h-3.5 w-3.5" />}Create backup</Button></div></CardContent></Card>
      </div>
      <div className="mt-5 flex items-center justify-between rounded-xl border border-border bg-card p-4"><p className="flex items-center gap-2 text-xs text-muted-foreground"><TimerReset className="h-4 w-4" />Changes affect future scheduling and maintenance cycles; active tests are not interrupted.</p><Button type="submit" disabled={saveMutation.isPending || !isDirty}>{saveMutation.isPending ? <Spinner /> : <Save className="h-4 w-4" />}Save settings</Button></div>
      <ConfirmDialog open={confirmCleanup} onOpenChange={setConfirmCleanup} title="Delete expired results now?" description="The currently saved retention policy will be applied immediately. Matching results and their diagnostics cannot be recovered unless you created a backup." confirmLabel="Run cleanup" destructive busy={cleanupMutation.isPending} onConfirm={() => cleanupMutation.mutate()} />
      <ConfirmDialog
        open={pendingConfiguration !== null}
        onOpenChange={(open) => { if (!open && !configurationImportMutation.isPending) setPendingConfiguration(null) }}
        title="Replace the saved configuration?"
        description={pendingConfiguration ? <div className="space-y-2"><p><span className="font-medium text-foreground">{pendingConfiguration.filename}</span> contains {pendingConfiguration.document.tasks.length} tasks and {pendingConfiguration.document.routeProfiles.length} route profiles.</p><p>Saved settings, tasks, and route profiles will be replaced. Measurement results and the Ookla terms acknowledgement remain unchanged. Import is blocked while a test is active.</p>{isDirty ? <p className="text-amber-500">Unsaved edits on this page will be discarded.</p> : null}</div> : ''}
        confirmLabel="Import configuration"
        destructive
        busy={configurationImportMutation.isPending}
        onConfirm={() => { if (pendingConfiguration) configurationImportMutation.mutate(pendingConfiguration.document) }}
      />
    </form>
  )
}
