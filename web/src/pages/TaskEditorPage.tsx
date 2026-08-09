import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, ArrowLeft, ArrowRight, Check, CheckCircle2, Clock3, Cloud, Gauge, ListChecks, Network, RadioTower, Save, Search, Server, ShieldCheck } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Controller, useForm, useWatch, type UseFormReturn } from 'react-hook-form'
import { Link, useNavigate, useParams } from 'react-router'
import { z } from 'zod'
import { api, getApiErrorMessage } from '../lib/api'
import { describeCron, ianaTimezones as commonTimezones, isValidTimezone, nextCronRuns, schedulePresets, validateCron } from '../lib/cron'
import { queryKeys } from '../lib/query'
import type { NetworkInterface, Provider, ProviderId, ProviderServer, RouteProfile, RouteValidation, Settings, Task, TaskInput } from '../lib/types'
import { cn, formatDateTime, providerLabel } from '../lib/utils'
import { useAppSettings } from '../hooks/useAppSettings'
import { PageHeader } from '../components/common/PageHeader'
import { ProviderBadge } from '../components/common/EntityBadges'
import { OoklaEulaAcceptance } from '../components/common/OoklaEulaAcceptance'
import { Badge, StatusDot } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { CheckField, Field, Input, NativeSelect, Textarea } from '../components/ui/fields'
import { EmptyState, ErrorState, LoadingState, Spinner } from '../components/ui/states'
import { useToast } from '../components/ui/toast'

const customEndpointError = 'Use a safe relative endpoint without a leading slash, traversal, backslash, encoded separator, control character, scheme, host, or fragment.'
export const customEndpointSchema = z.string().max(2048)
  .refine((value) => !Array.from(value).some(isURLControlCharacter), customEndpointError)
  .transform((value) => value.trim())
  .refine(isSafeCustomEndpoint, customEndpointError)

export const taskSchema = z.object({
  name: z.string().trim().min(2, 'Enter at least 2 characters.').max(100),
  description: z.string().trim().max(500),
  enabled: z.boolean(),
  provider: z.enum(['ookla', 'librespeed', 'cloudflare']),
  cronExpression: z.string().trim().refine(validateCron, 'Enter a valid five-field cron expression.'),
  timezone: z.string().trim().min(1, 'Select a timezone.').refine(isValidTimezone, 'Select a valid IANA timezone.'),
  randomJitterSeconds: z.number().int().min(0).max(3600),
  serverSelectionMode: z.enum(['automatic', 'fixed', 'custom']),
  serverId: z.string().trim().max(200),
  serverUrl: z.string().trim().max(2048),
  customServerName: z.string().trim().max(200),
  customDownloadPath: customEndpointSchema,
  customUploadPath: customEndpointSchema,
  customPingPath: customEndpointSchema,
  customIpPath: customEndpointSchema,
  allowInsecureCustomServer: z.boolean(),
  interfaceName: z.string().trim().min(1, 'Select a concrete interface.'),
  sourceIp: z.string().trim().min(1, 'Select one concrete source address.'),
  ipFamily: z.enum(['auto', 'ipv4', 'ipv6']),
  routeProfileId: z.string(),
  timeoutSeconds: z.number().int().min(5).max(3600),
  preventOverlap: z.boolean(),
  routeValidation: z.enum(['required', 'interface-only']),
  skipTlsVerification: z.boolean(),
}).superRefine((value, context) => {
  if (value.provider === 'cloudflare' && value.serverSelectionMode !== 'automatic') context.addIssue({ code: 'custom', path: ['serverSelectionMode'], message: 'Cloudflare always uses automatic edge selection.' })
  if (value.serverSelectionMode === 'fixed' && !value.serverId) context.addIssue({ code: 'custom', path: ['serverId'], message: 'Select or enter a server ID.' })
  if (value.serverSelectionMode === 'custom') {
    if (value.provider !== 'librespeed') context.addIssue({ code: 'custom', path: ['serverSelectionMode'], message: 'Custom URLs are supported by LibreSpeed only.' })
    const parsed = z.url({ protocol: /^https?$/ }).safeParse(value.serverUrl)
    if (!parsed.success) context.addIssue({ code: 'custom', path: ['serverUrl'], message: 'Enter a valid HTTP or HTTPS URL.' })
    if (parsed.success && parsed.data.toLowerCase().startsWith('http:') && !value.allowInsecureCustomServer) context.addIssue({ code: 'custom', path: ['allowInsecureCustomServer'], message: 'Explicitly allow plain HTTP or use HTTPS.' })
  }
})

export type TaskFormValues = z.infer<typeof taskSchema>

const defaults: TaskFormValues = {
  name: '', description: '', enabled: true, provider: 'cloudflare', cronExpression: '0 */6 * * *', timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', randomJitterSeconds: 0,
  serverSelectionMode: 'automatic', serverId: '', serverUrl: '', customServerName: '', customDownloadPath: '', customUploadPath: '', customPingPath: '', customIpPath: '', allowInsecureCustomServer: false,
  interfaceName: '', sourceIp: '', ipFamily: 'auto', routeProfileId: '', timeoutSeconds: 120,
  preventOverlap: true, routeValidation: 'required', skipTlsVerification: false,
}

function newTaskFormValues(settings: Settings): TaskFormValues {
  return { ...defaults, timezone: settings.defaultTimezone || 'UTC', timeoutSeconds: settings.defaultTaskTimeoutSeconds }
}

function taskToFormValues(task: Task): TaskFormValues {
  return {
    name: task.name,
    description: task.description,
    enabled: task.enabled,
    provider: task.provider,
    cronExpression: task.cronExpression,
    timezone: task.timezone,
    randomJitterSeconds: task.randomJitterSeconds,
    serverSelectionMode: task.serverSelectionMode,
    serverId: task.serverId,
    serverUrl: task.serverUrl,
    customServerName: customString(task.customServerDefinition, 'name'),
    customDownloadPath: customString(task.customServerDefinition, 'dlURL'),
    customUploadPath: customString(task.customServerDefinition, 'ulURL'),
    customPingPath: customString(task.customServerDefinition, 'pingURL'),
    customIpPath: customString(task.customServerDefinition, 'getIpURL'),
    allowInsecureCustomServer: task.customServerDefinition.allowInsecure === true,
    interfaceName: task.interfaceName,
    sourceIp: task.sourceIp,
    ipFamily: task.ipFamily,
    routeProfileId: task.routeProfileId ?? '',
    timeoutSeconds: task.timeoutSeconds,
    preventOverlap: task.preventOverlap,
    routeValidation: task.routeValidation,
    skipTlsVerification: task.providerOptions.skipTlsVerification === true,
  }
}

export function buildTaskInput(data: TaskFormValues, existing?: Task): TaskInput {
  const selectionMode = data.provider === 'cloudflare' ? 'automatic' : data.serverSelectionMode
  const customServerDefinition = { ...(existing?.customServerDefinition ?? {}) }
  if (data.provider === 'librespeed') {
    setCustomString(customServerDefinition, 'name', data.customServerName)
    setCustomString(customServerDefinition, 'dlURL', data.customDownloadPath)
    setCustomString(customServerDefinition, 'ulURL', data.customUploadPath)
    setCustomString(customServerDefinition, 'pingURL', data.customPingPath)
    setCustomString(customServerDefinition, 'getIpURL', data.customIpPath)
    if (data.allowInsecureCustomServer) customServerDefinition.allowInsecure = true
    else delete customServerDefinition.allowInsecure
  }
  const providerOptions = { ...(existing?.providerOptions ?? {}) }
  if (data.provider === 'librespeed') {
    providerOptions.telemetry = false
    providerOptions.skipTlsVerification = data.skipTlsVerification
  }
  return {
    name: data.name,
    description: data.description,
    enabled: data.enabled,
    provider: data.provider,
    cronExpression: data.cronExpression,
    timezone: data.timezone,
    randomJitterSeconds: data.randomJitterSeconds,
    serverSelectionMode: selectionMode,
    serverId: selectionMode === 'fixed' ? data.serverId : '',
    serverUrl: selectionMode === 'custom' ? data.serverUrl : '',
    customServerDefinition,
    interfaceName: data.interfaceName,
    sourceIp: data.sourceIp,
    ipFamily: data.ipFamily,
    routeProfileId: data.routeProfileId || null,
    timeoutSeconds: data.timeoutSeconds,
    providerOptions,
    preventOverlap: data.preventOverlap,
    routeValidation: data.routeValidation,
  }
}

function customString(values: Record<string, unknown>, key: string): string {
  return typeof values[key] === 'string' ? values[key] : ''
}

function setCustomString(values: Record<string, unknown>, key: string, value: string): void {
  if (value) values[key] = value
  else values[key] = undefined
}

function taskInputSignature(input: TaskInput): string {
  return JSON.stringify(input)
}

function isSafeCustomEndpoint(value: string): boolean {
  if (!value) return true
  const lower = value.toLowerCase()
  if (value.startsWith('/') || value.includes('\\') || lower.includes('%2f') || lower.includes('%5c') || lower.includes('%00') || lower.includes('%0a') || lower.includes('%0d') || value.includes('#') || /^[A-Za-z][A-Za-z0-9+.-]*:/.test(value)) return false

  const path = value.split('?', 1)[0]
  if (!path) return false
  for (const segment of path.split('/')) {
    if (!segment) return false
    let decoded: string
    try {
      decoded = decodeURIComponent(segment)
    } catch {
      return false
    }
    if (decoded === '.' || decoded === '..') return false
  }

  try {
    const parsed = new URL(value, 'https://multispeed.invalid/')
    return parsed.origin === 'https://multispeed.invalid' && !parsed.username && !parsed.password && !parsed.hash
  } catch {
    return false
  }
}

function isURLControlCharacter(character: string): boolean {
  const code = character.charCodeAt(0)
  return code <= 0x1f || code === 0x7f
}

const steps = [
  { title: 'Identity & provider', description: 'Name the test and select its measurement methodology.', icon: Gauge },
  { title: 'Target & network', description: 'Pin the server and concrete Linux source path.', icon: Network },
  { title: 'Schedule & safety', description: 'Control timing, timeout, jitter, and overlap.', icon: Clock3 },
  { title: 'Review & validate', description: 'Confirm the persisted task before activation.', icon: ShieldCheck },
] as const

const stepFields: Record<number, Array<keyof TaskFormValues>> = {
  0: ['name', 'description', 'provider'],
  1: ['serverSelectionMode', 'serverId', 'serverUrl', 'customServerName', 'customDownloadPath', 'customUploadPath', 'customPingPath', 'customIpPath', 'allowInsecureCustomServer', 'interfaceName', 'sourceIp', 'ipFamily', 'routeProfileId'],
  2: ['cronExpression', 'timezone', 'randomJitterSeconds', 'timeoutSeconds', 'preventOverlap', 'routeValidation'],
  3: [],
}

export default function TaskEditorPage() {
  const { taskId } = useParams()
  const editing = Boolean(taskId)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [step, setStep] = useState(0)
  const [showVirtualInterfaces, setShowVirtualInterfaces] = useState(false)
  const [cronRuns, setCronRuns] = useState<Date[]>([])
  const [preflight, setPreflight] = useState<{ signature: string; validation: RouteValidation } | null>(null)
  const settingsQuery = useAppSettings()
  const taskQuery = useQuery({ queryKey: queryKeys.task(taskId ?? 'new'), queryFn: () => api.task(taskId ?? ''), enabled: editing })
  const interfacesQuery = useQuery({ queryKey: [...queryKeys.interfaces, 'task-editor', true, true], queryFn: () => api.interfaces({ includeDown: true, includeVirtual: true }) })
  const routesQuery = useQuery({ queryKey: queryKeys.routes, queryFn: api.routes })
  const providersQuery = useQuery({ queryKey: queryKeys.providers, queryFn: api.providers })
  const formValues = useMemo(() => taskQuery.data ? taskToFormValues(taskQuery.data) : newTaskFormValues(settingsQuery.settings), [settingsQuery.settings, taskQuery.data])
  const form = useForm<TaskFormValues>({ resolver: zodResolver(taskSchema), defaultValues: formValues, values: formValues, resetOptions: { keepDirtyValues: true }, mode: 'onBlur' })
  const values = useWatch({ control: form.control })
  const selectedProvider = values.provider ?? 'cloudflare'
  const selectedInterface = values.interfaceName ?? ''
  const cronExpression = values.cronExpression ?? ''
  const timezone = values.timezone ?? 'UTC'

  useEffect(() => {
    let active = true
    void nextCronRuns(cronExpression, timezone).then((items) => active && setCronRuns(items))
    return () => { active = false }
  }, [cronExpression, timezone])

  const saveMutation = useMutation({
    mutationFn: (input: TaskInput) => editing && taskId ? api.updateTask(taskId, input) : api.createTask(input),
    onSuccess: (task) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.tasks })
      toast({ tone: 'success', title: editing ? 'Task updated' : 'Task created', description: `${task.name} was persisted and its schedule recalculated.` })
      void navigate('/tasks')
    },
    onError: (error) => toast({ tone: 'error', title: editing ? 'Unable to update task' : 'Unable to create task', description: getApiErrorMessage(error) }),
  })

  const preflightMutation = useMutation({
    mutationFn: async ({ input, signature }: { input: TaskInput; signature: string }) => ({ signature, validation: await api.validateTaskInput(input) }),
    onMutate: () => setPreflight(null),
    onSuccess: (result) => {
      setPreflight(result)
      toast({ tone: result.validation.success ? 'success' : 'error', title: result.validation.success ? 'Current task path validated' : 'Task preflight failed', description: result.validation.message })
    },
    onError: (error) => { setPreflight(null); toast({ tone: 'error', title: 'Task preflight failed', description: getApiErrorMessage(error) }) },
  })

  const onSubmit = (data: TaskFormValues) => {
    const input = buildTaskInput(data, taskQuery.data)
    const signature = taskInputSignature(input)
    if (data.enabled && data.provider !== 'ookla' && (preflight?.signature !== signature || !preflight.validation.success)) {
      setStep(steps.length - 1)
      toast({ tone: 'error', title: 'Validate this enabled task first', description: 'Run a successful preflight for the current provider, target, and source path before saving.' })
      return
    }
    saveMutation.mutate(input)
  }

  const validateCandidate = async () => {
    if (!await form.trigger()) {
      toast({ tone: 'error', title: 'Complete required task fields', description: 'Resolve the highlighted fields before route preflight.' })
      return
    }
    const input = buildTaskInput(form.getValues(), taskQuery.data)
    preflightMutation.mutate({ input, signature: taskInputSignature(input) })
  }

  const nextStep = async () => {
    const valid = await form.trigger(stepFields[step] ?? [])
    if (valid) setStep((current) => Math.min(current + 1, steps.length - 1))
  }

  const firstError = taskQuery.error ?? interfacesQuery.error ?? routesQuery.error ?? providersQuery.error ?? settingsQuery.error
  if ((editing && taskQuery.isLoading) || interfacesQuery.isLoading || routesQuery.isLoading || providersQuery.isLoading || settingsQuery.isLoading) return <LoadingState label="Loading provider and network capabilities…" />
  if (firstError) return <ErrorState error={firstError} onRetry={() => { void taskQuery.refetch(); void interfacesQuery.refetch(); void routesQuery.refetch(); void providersQuery.refetch(); void settingsQuery.refetch() }} />

  const providers = providersQuery.data ?? fallbackProviders
  const allInterfaces = interfacesQuery.data ?? []
  const interfaces = allInterfaces.filter((item) => !item.virtual || showVirtualInterfaces || item.name === selectedInterface)
  const routes = routesQuery.data ?? []
  const activeProvider = providers.find((provider) => provider.id === selectedProvider)
  const interfaceInfo = allInterfaces.find((item) => item.name === selectedInterface)
  const addressOptions = interfaceInfo?.addresses.filter((address) => !address.linkLocal && (values.ipFamily === 'auto' || address.family === values.ipFamily)) ?? []
  const currentInput = buildTaskInput(values as TaskFormValues, taskQuery.data)
  const currentSignature = taskInputSignature(currentInput)
  const currentPreflight = preflight?.signature === currentSignature ? preflight.validation : null

  return (
    <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
      <PageHeader title={editing ? `Edit ${taskQuery.data?.name ?? 'task'}` : 'Create an independent speed test'} description="The selected interface and source address are mandatory. MultiSpeed never falls back to another WAN when binding or route validation fails." actions={<Button asChild variant="outline"><Link to="/tasks"><ArrowLeft className="h-4 w-4" />Back to tasks</Link></Button>} />
      {editing && taskQuery.data?.networkPathValid === false ? <div role="alert" className="mb-5 flex gap-3 rounded-xl border border-rose-500/25 bg-rose-500/[.06] p-4 text-rose-700 dark:text-rose-300"><AlertTriangle className="mt-0.5 h-5 w-5 shrink-0" /><div><p className="text-sm font-semibold">This task's network path is no longer valid</p><p className="mt-1 text-xs leading-5">{taskQuery.data.networkPathMessage || 'The configured interface or source address is not available in the active Linux namespace.'} Select a current interface and concrete source address before saving or running this task.</p></div></div> : null}

      <div className="grid gap-5 lg:grid-cols-[250px_minmax(0,1fr)]">
        <Card className="h-fit p-3 lg:sticky lg:top-24">
          <ol className="space-y-1" aria-label="Task editor steps">
            {steps.map((item, index) => {
              const Icon = item.icon
              return <li key={item.title}><button type="button" onClick={() => setStep(index)} className={cn('flex w-full items-start gap-3 rounded-lg p-3 text-left transition focus-visible:ring-2 focus-visible:ring-ring', step === index ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground')} aria-current={step === index ? 'step' : undefined}><span className={cn('grid h-7 w-7 shrink-0 place-items-center rounded-lg border text-[11px] font-bold', step === index ? 'border-primary/30 bg-primary/10 text-primary' : index < step ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-500' : 'border-border bg-background')}>{index < step ? <Check className="h-3.5 w-3.5" /> : <Icon className="h-3.5 w-3.5" />}</span><span><span className="block text-xs font-semibold">{item.title}</span><span className="mt-0.5 block text-[10px] leading-4 opacity-70">{item.description}</span></span></button></li>
            })}
          </ol>
        </Card>

        <div className="min-w-0">
          {step === 0 ? <IdentityStep form={form} providers={providers} activeProvider={activeProvider} /> : null}
          {step === 1 ? <NetworkStep form={form} provider={activeProvider} interfaces={interfaces} routes={routes} addressOptions={addressOptions} showVirtualInterfaces={showVirtualInterfaces} onShowVirtualInterfacesChange={setShowVirtualInterfaces} /> : null}
          {step === 2 ? <ScheduleStep form={form} cronRuns={cronRuns} /> : null}
          {step === 3 ? <ReviewStep values={values as TaskFormValues} provider={activeProvider} route={routes.find((item) => item.id === values.routeProfileId)} cronRuns={cronRuns} preflight={currentPreflight} configurationChanged={preflight !== null && currentPreflight === null} validating={preflightMutation.isPending} onValidate={() => void validateCandidate()} /> : null}

          <div className="mt-5 flex items-center justify-between rounded-xl border border-border bg-card p-4 shadow-panel">
            <Button variant="outline" onClick={() => setStep((current) => Math.max(current - 1, 0))} disabled={step === 0 || saveMutation.isPending}><ArrowLeft className="h-4 w-4" />Previous</Button>
            <div className="text-center text-[11px] text-muted-foreground">Step {step + 1} of {steps.length}</div>
            {step < steps.length - 1 ? <Button onClick={(event) => { event.preventDefault(); void nextStep() }}>Continue<ArrowRight className="h-4 w-4" /></Button> : <Button type="submit" disabled={saveMutation.isPending}>{saveMutation.isPending ? <Spinner /> : <Save className="h-4 w-4" />}{editing ? 'Save changes' : 'Create task'}</Button>}
          </div>
        </div>
      </div>
    </form>
  )
}

type FormApi = UseFormReturn<TaskFormValues>

function IdentityStep({ form, providers, activeProvider }: { form: FormApi; providers: Provider[]; activeProvider?: Provider | undefined }) {
  const { register, control, formState: { errors } } = form
  return (
    <Card>
      <CardHeader><CardTitle>Identity & measurement provider</CardTitle><p className="text-xs leading-5 text-muted-foreground">Provider behavior remains isolated per task; changing this task cannot affect another schedule.</p></CardHeader>
      <CardContent className="space-y-5">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Task name" required error={errors.name?.message}><Input {...register('name')} placeholder="Fiber uplink · Frankfurt" autoFocus /></Field>
          <Controller control={control} name="enabled" render={({ field }) => <CheckField checked={field.value} onChange={field.onChange} label="Task enabled" description="Schedule immediately after saving." />} />
        </div>
        <Field label="Description" error={errors.description?.message} hint="Optional"><Textarea {...register('description')} placeholder="Primary WAN baseline during business hours." /></Field>
        <div>
          <p className="mb-2 text-xs font-semibold text-foreground">Provider <span className="text-rose-500">*</span></p>
          <Controller control={control} name="provider" render={({ field }) => <div className="grid gap-3 md:grid-cols-3">{providers.map((provider) => {
            const Icon = provider.id === 'ookla' ? Gauge : provider.id === 'librespeed' ? Server : Cloud
            return <button key={provider.id} type="button" onClick={() => { field.onChange(provider.id); if (provider.id === 'cloudflare') form.setValue('serverSelectionMode', 'automatic'); else if (form.getValues('serverSelectionMode') === 'custom' && provider.id === 'ookla') form.setValue('serverSelectionMode', 'automatic') }} className={cn('rounded-xl border p-4 text-left transition focus-visible:ring-2 focus-visible:ring-ring', field.value === provider.id ? 'border-primary/50 bg-primary/[.07] shadow-sm' : 'border-border bg-background hover:border-primary/25', !provider.available && 'opacity-65')} aria-pressed={field.value === provider.id}><div className="flex items-center justify-between gap-2"><span className={cn('grid h-9 w-9 place-items-center rounded-lg', provider.id === 'ookla' ? 'bg-cyan-500/10 text-cyan-500' : provider.id === 'librespeed' ? 'bg-violet-500/10 text-violet-500' : 'bg-orange-500/10 text-orange-500')}><Icon className="h-4 w-4" /></span><Badge tone={provider.available ? 'success' : 'danger'} className="gap-1"><StatusDot active={provider.available} />{provider.available ? 'Available' : 'Unavailable'}</Badge></div><p className="mt-3 text-sm font-semibold">{provider.displayName}</p><p className="mt-1 text-[11px] leading-4 text-muted-foreground">{provider.id === 'cloudflare' ? 'Native edge methodology with automatic colo selection.' : provider.id === 'ookla' ? 'Official CLI with broad fixed-server discovery.' : 'Open-source CLI with public and custom backends.'}</p></button>
          })}</div>} />
        </div>
        {activeProvider && !activeProvider.available ? <div className="flex gap-3 rounded-lg border border-amber-500/25 bg-amber-500/[.07] p-4 text-sm"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-500" /><div className="min-w-0 flex-1"><p className="font-semibold text-foreground">{activeProvider.displayName} is not available</p><p className="mt-1 text-xs leading-5 text-muted-foreground">{activeProvider.message || 'Check provider installation details on System information.'}</p>{activeProvider.id === 'ookla' ? <OoklaEulaAcceptance compact /> : null}</div></div> : null}
      </CardContent>
    </Card>
  )
}

function NetworkStep({ form, provider, interfaces, routes, addressOptions, showVirtualInterfaces, onShowVirtualInterfacesChange }: { form: FormApi; provider?: Provider | undefined; interfaces: NetworkInterface[]; routes: RouteProfile[]; addressOptions: NetworkInterface['addresses']; showVirtualInterfaces: boolean; onShowVirtualInterfacesChange: (show: boolean) => void }) {
  const { register, control, setValue, formState: { errors } } = form
  const mode = useWatch({ control, name: 'serverSelectionMode' })
  const selectedProvider = useWatch({ control, name: 'provider' })
  const selectedInterface = useWatch({ control, name: 'interfaceName' })
  const selectedSource = useWatch({ control, name: 'sourceIp' })
  const ipFamily = useWatch({ control, name: 'ipFamily' })
  const selectedServerId = useWatch({ control, name: 'serverId' })
  const skipTls = useWatch({ control, name: 'skipTlsVerification' })
  const allowInsecure = useWatch({ control, name: 'allowInsecureCustomServer' })
  return (
    <div className="space-y-5">
      <Card>
        <CardHeader><CardTitle>Target selection</CardTitle><p className="text-xs leading-5 text-muted-foreground">Discovery uses the same network binding as the final measurement.</p></CardHeader>
        <CardContent className="space-y-5">
          {selectedProvider === 'cloudflare' ? <div className="flex items-start gap-3 rounded-xl border border-orange-500/25 bg-orange-500/[.06] p-4"><Cloud className="mt-0.5 h-5 w-5 shrink-0 text-orange-500" /><div><p className="text-sm font-semibold">Automatic edge selection</p><p className="mt-1 text-xs leading-5 text-muted-foreground">Cloudflare selects the reachable edge colo for this source path. Its native multi-sample methodology differs from Ookla and LibreSpeed.</p></div></div> : <>
            <Controller control={control} name="serverSelectionMode" render={({ field }) => <div className="grid gap-2 sm:grid-cols-3">{[
              { value: 'automatic', label: 'Automatic', description: 'Provider selects a target' },
              { value: 'fixed', label: 'Fixed server', description: 'Persist a discovered ID' },
              ...(selectedProvider === 'librespeed' && provider?.capabilities.customServerUrls ? [{ value: 'custom', label: 'Custom backend', description: 'Use your own HTTPS URL' }] : []),
            ].map((item) => <button key={item.value} type="button" onClick={() => field.onChange(item.value)} className={cn('rounded-lg border p-3 text-left transition', field.value === item.value ? 'border-primary/50 bg-primary/[.07]' : 'border-border bg-background hover:border-primary/25')}><span className="block text-xs font-semibold">{item.label}</span><span className="mt-0.5 block text-[10px] text-muted-foreground">{item.description}</span></button>)}</div>} />
            {selectedProvider === 'librespeed' && !provider?.capabilities.customServerUrls ? <div className="flex gap-2 rounded-lg border border-amber-500/25 bg-amber-500/[.06] p-3 text-xs leading-5 text-amber-700 dark:text-amber-300"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />Custom backends are disabled by this deployment. Add the exact base URL to APP_ALLOWED_CUSTOM_SERVER_URLS and restart MultiSpeed.</div> : null}
            {mode === 'fixed' ? <ServerPicker provider={selectedProvider} interfaceName={selectedInterface} sourceIp={selectedSource} ipFamily={ipFamily} value={selectedServerId} onChange={(id) => setValue('serverId', id, { shouldValidate: true, shouldDirty: true })} error={errors.serverId?.message} /> : null}
            {mode === 'custom' ? <div className="space-y-4"><div className="grid gap-4 md:grid-cols-2"><Field label="LibreSpeed backend URL" required error={errors.serverUrl?.message}><Input type="url" {...register('serverUrl')} placeholder="https://speed.example.net" /></Field><Field label="Display name" hint="Optional" error={errors.customServerName?.message}><Input {...register('customServerName')} placeholder="Office LibreSpeed" /></Field><Field label="Download endpoint" hint="Relative to backend URL" error={errors.customDownloadPath?.message}><Input {...register('customDownloadPath')} placeholder="garbage.php" /></Field><Field label="Upload endpoint" hint="Relative to backend URL" error={errors.customUploadPath?.message}><Input {...register('customUploadPath')} placeholder="empty.php" /></Field><Field label="Ping endpoint" hint="Relative to backend URL" error={errors.customPingPath?.message}><Input {...register('customPingPath')} placeholder="empty.php" /></Field><Field label="Public-IP endpoint" hint="Relative to backend URL" error={errors.customIpPath?.message}><Input {...register('customIpPath')} placeholder="getIP.php" /></Field></div><div className="grid gap-3 md:grid-cols-2"><Controller control={control} name="allowInsecureCustomServer" render={({ field }) => <CheckField checked={field.value} onChange={field.onChange} label="Allow plain HTTP backend" description="Unsafe. Required only when the custom URL starts with http://." />} /><Controller control={control} name="skipTlsVerification" render={({ field }) => <CheckField checked={field.value} onChange={field.onChange} label="Skip TLS certificate verification" description="Unsafe. Enable only for a backend you control; every result records this setting." />} /></div>{errors.allowInsecureCustomServer?.message ? <p className="text-xs text-rose-600 dark:text-rose-400">{errors.allowInsecureCustomServer.message}</p> : null}{allowInsecure ? <div className="flex gap-2 rounded-lg border border-rose-500/25 bg-rose-500/[.06] p-3 text-xs leading-5 text-rose-700 dark:text-rose-300"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />Plain HTTP permits interception and response tampering. Use this only on an isolated network you control.</div> : null}{skipTls ? <div className="flex gap-2 rounded-lg border border-rose-500/25 bg-rose-500/[.06] p-3 text-xs leading-5 text-rose-700 dark:text-rose-300"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />TLS verification is disabled for this task. Traffic can be intercepted or redirected.</div> : null}</div> : null}
          </>}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Concrete source binding</CardTitle><p className="text-xs leading-5 text-muted-foreground">No arbitrary address is selected and there is no fallback to the default route.</p></CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-start justify-between gap-4 rounded-lg border border-border bg-muted/30 p-3">
            <div><p className="text-xs font-semibold">Virtual network paths</p><p className="mt-1 text-[11px] leading-5 text-muted-foreground">Show VPN, tunnel, bridge, and other virtual interfaces when they are intentional WAN paths.</p></div>
            <CheckField checked={showVirtualInterfaces} onChange={onShowVirtualInterfacesChange} label="Show virtual paths" />
          </div>
          <div className="grid gap-4 md:grid-cols-3">
            <Field label="IP family" required error={errors.ipFamily?.message}><NativeSelect {...register('ipFamily')} onChange={(event) => { void register('ipFamily').onChange(event); setValue('sourceIp', ''); setValue('routeProfileId', '') }}><option value="auto">IPv4 or IPv6</option><option value="ipv4">IPv4 only</option><option value="ipv6">IPv6 only</option></NativeSelect></Field>
            <Field label="Linux interface" required error={errors.interfaceName?.message}><NativeSelect {...register('interfaceName')} onChange={(event) => { void register('interfaceName').onChange(event); setValue('sourceIp', '', { shouldValidate: true }); setValue('routeProfileId', '') }}><option value="">Select interface…</option>{interfaces.map((item) => <option key={item.name} value={item.name}>{item.name} · {item.operationalState || (item.operational ? 'up' : 'down')}{item.virtual ? ' · virtual' : ''}</option>)}</NativeSelect></Field>
            <Field label="Source address" required error={errors.sourceIp?.message}><NativeSelect {...register('sourceIp')} onChange={(event) => { void register('sourceIp').onChange(event); setValue('routeProfileId', '') }} disabled={!selectedInterface}><option value="">Select concrete address…</option>{addressOptions.map((item) => <option key={item.address} value={item.address}>{item.address} · {item.family.toUpperCase()}</option>)}</NativeSelect></Field>
          </div>
          {selectedInterface && addressOptions.length === 0 ? <p className="rounded-lg border border-amber-500/25 bg-amber-500/[.06] p-3 text-xs text-amber-700 dark:text-amber-300">This interface has no non-link-local {ipFamily === 'auto' ? '' : ipFamily.toUpperCase()} address. Choose another interface or family.</p> : null}
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="Route profile" hint="Recommended" error={errors.routeProfileId?.message}><NativeSelect {...register('routeProfileId')}><option value="">No persisted route expectation</option>{routes.map((route) => { const pathMatches = route.interfaceName === selectedInterface && route.sourceIp === selectedSource; return <option key={route.id} value={route.id} disabled={!pathMatches}>{route.name} · {route.interfaceName} · {route.sourceIp}{pathMatches ? '' : ' · path mismatch'}</option> })}</NativeSelect></Field>
            <div className="rounded-lg border border-border bg-muted/35 p-3"><p className="text-xs font-semibold">Binding capability</p><p className="mt-1 text-[11px] leading-5 text-muted-foreground">{provider?.capabilities.sourceAddressBinding ? 'Provider supports source-address binding.' : provider?.capabilities.interfaceBinding ? 'Provider binds to the selected interface.' : 'Review provider availability before enabling this task.'}</p></div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function ServerPicker({ provider, interfaceName, sourceIp, ipFamily, value, onChange, error }: { provider: ProviderId; interfaceName: string; sourceIp: string; ipFamily: TaskFormValues['ipFamily']; value: string; onChange: (id: string) => void; error?: string | undefined }) {
  const [search, setSearch] = useState('')
  const serversQuery = useQuery({ queryKey: ['provider-servers', provider, search, interfaceName, sourceIp, ipFamily], queryFn: () => api.providerServers(provider, { search, interfaceName, sourceIp, ipFamily }), enabled: false })
  const discover = () => void serversQuery.refetch()
  return (
    <div className="space-y-3">
      <Field label={`${providerLabel(provider)} server`} required error={error} hint="Discovery follows selected WAN">
        <div className="flex gap-2"><div className="relative flex-1"><Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" /><Input className="pl-9" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="City, sponsor, host, or server ID" onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); discover() } }} /></div><Button variant="outline" onClick={discover} disabled={!interfaceName || !sourceIp || serversQuery.isFetching}>{serversQuery.isFetching ? <Spinner /> : <RadioTower className="h-4 w-4" />}Discover</Button></div>
      </Field>
      {!interfaceName || !sourceIp ? <p className="text-xs text-muted-foreground">Select an interface and concrete source address before discovery.</p> : null}
      {serversQuery.error ? <p className="rounded-lg border border-rose-500/20 bg-rose-500/[.06] p-3 text-xs text-rose-600 dark:text-rose-400">{getApiErrorMessage(serversQuery.error)}</p> : null}
      {(serversQuery.data?.length ?? 0) > 0 ? <div className="max-h-64 overflow-y-auto rounded-lg border border-border scrollbar-thin" role="listbox" aria-label="Discovered servers">{serversQuery.data?.map((server) => <ServerOption key={server.id} server={server} selected={server.id === value} onSelect={() => onChange(server.id)} />)}</div> : serversQuery.isFetched && !serversQuery.isFetching && !serversQuery.error ? <EmptyState compact title="No servers found" description="Try a broader search or verify that discovery can use this source path." /> : null}
      {value ? <div className="flex items-center gap-2 text-xs text-muted-foreground"><CheckCircle2 className="h-4 w-4 text-emerald-500" />Persisted server ID: <code className="rounded bg-muted px-1.5 py-0.5 text-foreground">{value}</code></div> : null}
    </div>
  )
}

function ServerOption({ server, selected, onSelect }: { server: ProviderServer; selected: boolean; onSelect: () => void }) {
  return <button type="button" role="option" aria-selected={selected} onClick={onSelect} className={cn('flex w-full items-center gap-3 border-b border-border p-3 text-left transition last:border-b-0 hover:bg-muted/50', selected && 'bg-primary/[.07]')}><span className={cn('grid h-8 w-8 shrink-0 place-items-center rounded-lg border', selected ? 'border-primary/30 bg-primary/10 text-primary' : 'border-border bg-background text-muted-foreground')}><Server className="h-4 w-4" /></span><span className="min-w-0 flex-1"><span className="block truncate text-xs font-semibold">{server.name || server.host}</span><span className="mt-0.5 block truncate text-[10px] text-muted-foreground">{server.sponsor} · {server.location}{server.country ? `, ${server.country}` : ''}</span></span><span className="font-mono text-[10px] text-muted-foreground">#{server.id}</span></button>
}

function ScheduleStep({ form, cronRuns }: { form: FormApi; cronRuns: Date[] }) {
  const { register, control, setValue, formState: { errors } } = form
  const cron = useWatch({ control, name: 'cronExpression' })
  const timezone = useWatch({ control, name: 'timezone' })
  const dateTime = (value: string) => `${formatDateTime(value, false, timezone)} (${timezone})`
  return (
    <div className="space-y-5">
      <Card>
        <CardHeader><CardTitle>Independent schedule</CardTitle><p className="text-xs leading-5 text-muted-foreground">Standard five-field cron syntax is evaluated in this task’s IANA timezone.</p></CardHeader>
        <CardContent className="space-y-5">
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">{schedulePresets.map((preset) => <button key={preset.id} type="button" onClick={() => setValue('cronExpression', preset.expression, { shouldValidate: true, shouldDirty: true })} className={cn('rounded-lg border p-3 text-left transition', cron === preset.expression ? 'border-primary/50 bg-primary/[.07]' : 'border-border bg-background hover:border-primary/25')}><span className="block text-xs font-semibold">{preset.label}</span><span className="mt-0.5 block text-[10px] leading-4 text-muted-foreground">{preset.description}</span></button>)}</div>
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="Advanced cron expression" required error={errors.cronExpression?.message} hint={describeCron(cron)}><Input className="font-mono" {...register('cronExpression')} placeholder="0 */6 * * *" spellCheck={false} /></Field>
            <Field label="Timezone" required error={errors.timezone?.message}><NativeSelect {...register('timezone')}>{commonTimezones.includes(timezone) ? null : <option value={timezone}>{timezone}</option>}{commonTimezones.map((zone) => <option key={zone} value={zone}>{zone}</option>)}</NativeSelect></Field>
          </div>
          <div className="rounded-xl border border-border bg-muted/30 p-4"><p className="mb-3 text-[11px] font-semibold uppercase tracking-[.1em] text-muted-foreground">Next five executions</p>{cronRuns.length ? <ol className="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">{cronRuns.map((date, index) => <li key={date.toISOString()} className="rounded-lg border border-border bg-background p-2.5"><span className="block text-[10px] text-muted-foreground">Run {index + 1}</span><span className="mt-0.5 block text-xs font-semibold tabular-nums">{dateTime(date.toISOString())}</span></li>)}</ol> : <p className="text-xs text-rose-600 dark:text-rose-400">A preview is unavailable until the expression and timezone are valid.</p>}</div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Execution guardrails</CardTitle><p className="text-xs leading-5 text-muted-foreground">Bound run duration and avoid competing tests on the same path.</p></CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Timeout" hint="5–3600 seconds" error={errors.timeoutSeconds?.message}><Input type="number" min={5} max={3600} {...register('timeoutSeconds', { valueAsNumber: true })} /></Field>
            <Field label="Random start jitter" hint="0–3600 seconds" error={errors.randomJitterSeconds?.message}><Input type="number" min={0} max={3600} {...register('randomJitterSeconds', { valueAsNumber: true })} /></Field>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <Controller control={control} name="preventOverlap" render={({ field }) => <CheckField checked={field.value} onChange={field.onChange} label="Prevent task overlap" description="Skip a new execution while this task is active." />} />
            <Field label="Route-validation behavior" required error={errors.routeValidation?.message}><NativeSelect {...register('routeValidation')}><option value="required">Full route validation · fail closed</option><option value="interface-only">Interface and source binding · fail closed</option></NativeSelect></Field>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function ReviewStep({ values, provider, route, cronRuns, preflight, configurationChanged, validating, onValidate }: { values: TaskFormValues; provider?: Provider | undefined; route?: RouteProfile | undefined; cronRuns: Date[]; preflight: RouteValidation | null; configurationChanged: boolean; validating: boolean; onValidate: () => void }) {
  const { toast } = useToast()
  const dateTime = (value: string) => `${formatDateTime(value, false, values.timezone)} (${values.timezone})`
  const routeMutation = useMutation({ mutationFn: () => route ? api.validateRoute(route.id) : Promise.reject(new Error('No route profile selected.')), onSuccess: (result) => toast({ tone: result.success ? 'success' : 'error', title: result.success ? 'Route profile validated' : 'Route mismatch detected', description: result.message }), onError: (error) => toast({ tone: 'error', title: 'Route validation failed', description: getApiErrorMessage(error) }) })
  const details = [
    { label: 'Provider', value: providerLabel(values.provider), extra: provider?.available ? 'Available' : 'Unavailable' },
    { label: 'Target', value: values.provider === 'cloudflare' ? 'Automatic edge selection' : values.serverSelectionMode === 'fixed' ? `Server #${values.serverId}` : values.serverSelectionMode === 'custom' ? values.serverUrl : 'Automatic provider selection' },
    { label: 'Source path', value: `${values.interfaceName || 'No interface'} · ${values.sourceIp || 'No address'}`, extra: values.ipFamily.toUpperCase() },
    { label: 'Route profile', value: route?.name ?? 'No persisted profile', extra: values.routeValidation },
    { label: 'Schedule', value: `${values.cronExpression} · ${values.timezone}`, extra: describeCron(values.cronExpression) },
    { label: 'Guardrails', value: `${values.timeoutSeconds}s timeout · ${values.randomJitterSeconds}s jitter`, extra: values.preventOverlap ? 'Overlap prevented' : 'Overlap allowed' },
  ]
  return (
    <Card>
      <CardHeader><CardTitle>Review this independent path</CardTitle><p className="text-xs leading-5 text-muted-foreground">Saving recalculates the persisted schedule immediately. Enabling it does not trigger an unscheduled speed test.</p></CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-col justify-between gap-3 rounded-xl border border-primary/25 bg-primary/[.055] p-4 sm:flex-row sm:items-center"><div><div className="flex items-center gap-2"><h3 className="text-base font-semibold">{values.name || 'Unnamed task'}</h3><ProviderBadge provider={values.provider} /><Badge tone={values.enabled ? 'success' : 'neutral'}>{values.enabled ? 'Enabled' : 'Disabled'}</Badge></div><p className="mt-1 text-xs text-muted-foreground">{values.description || 'No description provided.'}</p></div><ListChecks className="h-7 w-7 shrink-0 text-primary" /></div>
        <dl className="grid gap-3 md:grid-cols-2">{details.map((item) => <div key={item.label} className="rounded-lg border border-border bg-background p-3"><dt className="text-[10px] font-semibold uppercase tracking-[.1em] text-muted-foreground">{item.label}</dt><dd className="mt-1 break-words text-xs font-semibold text-foreground">{item.value}</dd>{item.extra ? <dd className="mt-0.5 text-[10px] capitalize text-muted-foreground">{item.extra}</dd> : null}</div>)}</dl>
        {values.provider === 'librespeed' && values.skipTlsVerification ? <div className="flex gap-2 rounded-lg border border-rose-500/25 bg-rose-500/[.06] p-3 text-xs text-rose-700 dark:text-rose-300"><AlertTriangle className="h-4 w-4 shrink-0" />TLS certificate verification is disabled and will be recorded in result metadata.</div> : null}
        <div role={preflight?.success ? 'status' : 'alert'} className={cn('flex gap-3 rounded-lg border p-3 text-xs leading-5', preflight?.success ? 'border-emerald-500/25 bg-emerald-500/[.06] text-emerald-700 dark:text-emerald-300' : preflight ? 'border-rose-500/25 bg-rose-500/[.06] text-rose-700 dark:text-rose-300' : 'border-amber-500/25 bg-amber-500/[.06] text-amber-700 dark:text-amber-300')}>
          {preflight?.success ? <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" /> : <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />}
          <div><p className="font-semibold">{preflight?.success ? 'Current configuration passed preflight' : preflight ? 'Current configuration failed preflight' : configurationChanged ? 'Configuration changed since preflight' : 'Current configuration has not been validated'}</p><p className="mt-0.5">{preflight?.message || (values.provider === 'ookla' ? 'Ookla tasks may be saved before the operator-installed CLI is available. Validation and execution remain blocked until the provider is ready.' : values.enabled ? 'A successful preflight is required before this enabled task can be saved.' : 'Disabled tasks may be saved without preflight, but validation is recommended before enabling.')}</p></div>
        </div>
        <div className="flex flex-wrap gap-2"><Button onClick={onValidate} disabled={validating}>{validating ? <Spinner /> : <RadioTower className="h-4 w-4" />}Validate current configuration</Button><Button variant="outline" onClick={() => routeMutation.mutate()} disabled={!route || routeMutation.isPending}>{routeMutation.isPending ? <Spinner /> : <ShieldCheck className="h-4 w-4" />}Validate route profile only</Button></div>
        <div className="rounded-lg border border-border bg-muted/30 p-3 text-xs text-muted-foreground">First scheduled execution: <strong className="font-semibold text-foreground">{cronRuns[0] ? dateTime(cronRuns[0].toISOString()) : 'Unavailable'}</strong></div>
      </CardContent>
    </Card>
  )
}

const fallbackCapabilities = { serverDiscovery: true, fixedServerIds: true, customServerUrls: false, interfaceBinding: true, sourceAddressBinding: true, ipv4: true, ipv6: true, jitter: true, packetLoss: false, resultUrls: false }
const fallbackProviders: Provider[] = [
  { id: 'ookla', displayName: 'Ookla Speedtest', available: true, version: '', message: '', capabilities: { ...fallbackCapabilities, packetLoss: true, resultUrls: true } },
  { id: 'librespeed', displayName: 'LibreSpeed', available: true, version: '', message: '', capabilities: { ...fallbackCapabilities, customServerUrls: true } },
  { id: 'cloudflare', displayName: 'Cloudflare', available: true, version: 'native', message: '', capabilities: { ...fallbackCapabilities, serverDiscovery: false, fixedServerIds: false } },
]
