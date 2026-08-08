import type {
  ApiErrorEnvelope,
  ConfigurationDocument,
  ConfigurationImportResult,
  DashboardSummary,
  NetworkInterface,
  Page,
  Provider,
  ProviderServer,
  Result,
  RouteProfile,
  RouteProfileInput,
  RouteValidation,
  Settings,
  StatisticsResponse,
  SystemInfo,
  Task,
  TaskInput,
} from './types'

const API_ROOT = '/api/v1'

export class ApiError extends Error {
  readonly code: string
  readonly status: number
  readonly requestId?: string
  readonly details?: Record<string, string[]>

  constructor(message: string, status: number, code = 'REQUEST_FAILED', requestId?: string, details?: Record<string, string[]>) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
    if (requestId !== undefined) this.requestId = requestId
    if (details !== undefined) this.details = details
  }
}

async function parseError(response: Response): Promise<ApiError> {
  try {
    const payload = (await response.json()) as Partial<ApiErrorEnvelope>
    if (payload.error?.message) {
      return new ApiError(payload.error.message, response.status, payload.error.code, payload.error.requestId, payload.error.details)
    }
  } catch {
    // A non-JSON upstream error is represented with the safe HTTP status text.
  }
  return new ApiError(response.statusText || 'The request could not be completed.', response.status)
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body !== undefined && !(init.body instanceof FormData)) headers.set('Content-Type', 'application/json')
  const response = await fetch(`${API_ROOT}${path}`, { ...init, headers, credentials: 'same-origin' })
  if (!response.ok) throw await parseError(response)
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

function queryString(params: object): string {
  const search = new URLSearchParams()
  Object.entries(params as Record<string, string | number | boolean | null | undefined | string[]>).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '' || (Array.isArray(value) && value.length === 0)) return
    if (Array.isArray(value)) value.forEach((item) => search.append(key, item))
    else search.set(key, String(value))
  })
  const value = search.toString()
  return value ? `?${value}` : ''
}

function itemsFrom<T>(payload: T[] | { items: T[] } | { data: T[] }): T[] {
  if (Array.isArray(payload)) return payload
  if ('items' in payload && Array.isArray(payload.items)) return payload.items
  if ('data' in payload && Array.isArray(payload.data)) return payload.data
  return []
}

export interface ResultsQuery {
  page?: number
  pageSize?: number
  sort?: string
  direction?: 'asc' | 'desc'
  taskId?: string
  provider?: string
  interfaceName?: string
  status?: string
  from?: string
  to?: string
}

export interface StatisticsQuery {
  granularity: 'raw' | 'day' | 'iso-week' | 'month' | 'year' | 'custom'
  metric?: string
  from?: string
  to?: string
  timezone?: string
  taskId?: string[]
  interfaceName?: string[]
  sourceIp?: string[]
  provider?: string[]
  serverId?: string[]
  routeProfileId?: string[]
  publicIp?: string[]
  groupBy?: string
}

async function resultsPage(params: ResultsQuery = {}): Promise<Page<Result>> {
  const { interfaceName, ...rest } = params
  const payload = await request<Page<Result> | Result[]>(`/results${queryString({ ...rest, interface: interfaceName })}`)
  if (Array.isArray(payload)) return { items: payload, page: 1, pageSize: payload.length, totalItems: payload.length, totalPages: 1 }
  return payload
}

export const api = {
  health: () => request<{ status: string }>('/healthz'),
  tasks: async () => itemsFrom(await request<Task[] | { items: Task[] } | { data: Task[] }>('/tasks')),
  task: (id: string) => request<Task>(`/tasks/${encodeURIComponent(id)}`),
  createTask: (input: TaskInput) => request<Task>('/tasks', { method: 'POST', body: JSON.stringify(input) }),
  updateTask: (id: string, input: TaskInput) => request<Task>(`/tasks/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(input) }),
  deleteTask: (id: string) => request<void>(`/tasks/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  runTask: (id: string) => request<Result | { resultId: string }>(`/tasks/${encodeURIComponent(id)}/run`, { method: 'POST' }),
  validateTask: (id: string) => requestValidation(`/tasks/${encodeURIComponent(id)}/validate`),
  validateTaskInput: (input: TaskInput) => requestValidation('/tasks/validate', input),
  duplicateTask: (id: string) => request<Task>(`/tasks/${encodeURIComponent(id)}/duplicate`, { method: 'POST' }),
  nextRuns: async (id: string) => {
    const payload = await request<string[] | { items?: string[]; nextRuns?: string[] }>(`/tasks/${encodeURIComponent(id)}/next-runs`)
    if (Array.isArray(payload)) return payload
    return payload.nextRuns ?? payload.items ?? []
  },
  results: resultsPage,
  dashboardSummary: () => request<DashboardSummary>('/results/dashboard-summary'),
  result: (id: string) => request<Result>(`/results/${encodeURIComponent(id)}`),
  deleteResult: (id: string) => request<void>(`/results/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  deleteResults: (ids: string[]) => request<{ deleted: number }>('/results/delete-batch', { method: 'POST', body: JSON.stringify({ ids }) }),
  statistics: (params: StatisticsQuery) => {
    const { interfaceName, ...rest } = params
    return request<StatisticsResponse | StatisticCompatibility | StatisticsApiReport>(`/statistics${queryString({ ...rest, metric: undefined, interface: interfaceName })}`).then(normalizeStatistics)
  },
  interfaces: async (options?: { includeDown?: boolean; includeVirtual?: boolean }) => itemsFrom(await request<NetworkInterface[] | { items: NetworkInterface[] }>(`/interfaces${queryString(options ?? {})}`)),
  refreshInterfaces: () => request<{ refreshedAt?: string; interfaces?: NetworkInterface[] }>('/interfaces/refresh', { method: 'POST' }),
  routes: async () => itemsFrom(await request<RouteProfile[] | { items: RouteProfile[] }>('/route-profiles')),
  route: (id: string) => request<RouteProfile>(`/route-profiles/${encodeURIComponent(id)}`),
  createRoute: (input: RouteProfileInput) => request<RouteProfile>('/route-profiles', { method: 'POST', body: JSON.stringify(input) }),
  updateRoute: (id: string, input: RouteProfileInput) => request<RouteProfile>(`/route-profiles/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(input) }),
  deleteRoute: (id: string) => request<void>(`/route-profiles/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  validateRoute: (id: string) => requestValidation(`/route-profiles/${encodeURIComponent(id)}/validate`),
  providers: async () => itemsFrom(await request<Array<Provider | RawProviderDescriptor> | { items: Array<Provider | RawProviderDescriptor> }>('/providers')).map(normalizeProvider),
  providerServers: async (provider: string, params: { search?: string; interfaceName?: string; sourceIp?: string; ipFamily?: string }) => {
    const { interfaceName, ...rest } = params
    return itemsFrom(await request<ProviderServer[] | { items: ProviderServer[] }>(`/providers/${encodeURIComponent(provider)}/servers${queryString({ ...rest, interface: interfaceName })}`))
  },
  validateServer: (provider: string, input: Record<string, unknown>) => request<{ success: boolean; message: string }>(`/providers/${encodeURIComponent(provider)}/validate-server`, { method: 'POST', body: JSON.stringify(input) }),
  settings: () => request<Settings>('/settings'),
  updateSettings: (input: Settings) => request<Settings>('/settings', { method: 'PUT', body: JSON.stringify(input) }),
  updateOoklaEula: (accepted: boolean, confirmed: boolean) => request<Settings>('/settings/ookla-eula', { method: 'PUT', body: JSON.stringify({ accepted, confirmed }) }),
  exportConfiguration: downloadConfiguration,
  importConfiguration: (input: ConfigurationDocument) => request<ConfigurationImportResult>('/config/import', { method: 'POST', body: JSON.stringify(input) }),
  backup: downloadBackup,
  cleanupResults: (before?: string) => request<{ deletedResults?: number; deleted?: number; batches?: number; durationMilliseconds?: number }>('/retention/cleanup', { method: 'POST', body: JSON.stringify(before ? { before } : {}) }),
  system: async (): Promise<SystemInfo> => {
    const value = await request<RawSystemInfo>('/system')
    return { ...value, providers: Array.isArray(value.providers) ? value.providers.map(normalizeProvider) : [], buildDate: value.buildDate || value.buildTime || '' }
  },
  exportUrl: (format: 'csv' | 'json', params: ResultsQuery = {}) => {
    const { interfaceName, ...rest } = params
    return `${API_ROOT}/exports/results.${format}${queryString({ ...rest, interface: interfaceName })}`
  },
  eventsUrl: `${API_ROOT}/events`,
}

interface StatisticCompatibility {
  items?: StatisticsResponse['buckets']
  data?: StatisticsResponse['buckets']
  buckets?: StatisticsResponse['buckets']
  overall?: StatisticsResponse['overall']
  totals?: StatisticsResponse['totals']
  summary?: StatisticsResponse['summary']
  series?: StatisticsResponse['series']
  timezone?: string
}

interface RawProviderDescriptor {
  id: Provider['id']
  displayName: string
  capabilities: Provider['capabilities']
  availability: { available: boolean; version: string; message: string }
  available?: boolean
  version?: string
  message?: string
}

type RawSystemInfo = Omit<SystemInfo, 'providers' | 'buildDate'> & {
  providers: Array<Provider | RawProviderDescriptor>
  buildDate?: string
  buildTime?: string
}

interface StatisticsApiSummary {
  count: number
  minimum: number | null
  maximum: number | null
  average: number | null
  median: number | null
  p95: number | null
  standardDeviation: number | null
}

interface StatisticsApiBucket {
  start: string
  end: string
  label: string
  resultId?: string
  counts: { total: number; successful: number; failed: number; skipped: number; cancelled: number; other: number }
  successRatePercent: number | null
  failureRatePercent: number | null
  metrics: {
    downloadBitsPerSecond: StatisticsApiSummary
    uploadBitsPerSecond: StatisticsApiSummary
    latencyMilliseconds: StatisticsApiSummary
    jitterMilliseconds: StatisticsApiSummary
    packetLossPercent: StatisticsApiSummary
    executionDurationMilliseconds: StatisticsApiSummary
  }
}

interface StatisticsApiReport {
  from: string
  to: string
  granularity: string
  reportingTimezone: string
  groupBy?: string
  totalResults: number
  overall: StatisticsApiBucket
  groups: Array<{ key: string; label: string; overall: StatisticsApiBucket; buckets: StatisticsApiBucket[] }>
}

function normalizeStatistics(payload: StatisticsResponse | StatisticCompatibility | StatisticsApiReport): StatisticsResponse {
  if ('groups' in payload && Array.isArray(payload.groups)) {
    const series = payload.groups.map((group) => ({
      id: group.key || 'all',
      name: group.label || 'All results',
      overall: normalizeStatisticsBucket(group.overall),
      buckets: group.buckets.map((bucket) => normalizeStatisticsBucket(bucket)),
    }))
    const overall = normalizeStatisticsBucket(payload.overall)
    const grouped = Boolean(payload.groupBy)
    return {
      buckets: grouped ? series.flatMap((item) => item.buckets.map((bucket) => ({ ...bucket, label: `${item.name} / ${bucket.label ?? bucket.bucket}` }))) : (series[0]?.buckets ?? []),
      overall,
      summary: overall,
      ...(grouped ? { series } : {}),
      timezone: payload.reportingTimezone,
      ...(payload.groupBy !== undefined ? { groupBy: payload.groupBy } : {}),
    }
  }
  const compatible = payload as StatisticCompatibility
  return {
    buckets: compatible.buckets ?? compatible.items ?? compatible.data ?? [],
    ...(compatible.overall !== undefined ? { overall: compatible.overall } : {}),
    ...(compatible.totals !== undefined ? { totals: compatible.totals } : {}),
    ...(compatible.summary !== undefined ? { summary: compatible.summary } : {}),
    ...(compatible.series !== undefined ? { series: compatible.series } : {}),
    ...(compatible.timezone !== undefined ? { timezone: compatible.timezone } : {}),
  }
}

function normalizeStatisticsBucket(bucket: StatisticsApiBucket): StatisticsResponse['buckets'][number] {
  const summary = (value: StatisticsApiSummary) => ({ min: value.minimum, max: value.maximum, average: value.average, median: value.median, p95: value.p95, standardDeviation: value.standardDeviation })
  return {
    bucket: bucket.start,
    ...(bucket.resultId !== undefined ? { resultId: bucket.resultId } : {}),
    timestamp: bucket.start,
    label: bucket.label,
    sampleCount: bucket.counts.total,
    successfulCount: bucket.counts.successful,
    failedCount: bucket.counts.failed,
    skippedCount: bucket.counts.skipped,
    ...(bucket.successRatePercent !== null ? { successRate: bucket.successRatePercent } : {}),
    download: summary(bucket.metrics.downloadBitsPerSecond),
    upload: summary(bucket.metrics.uploadBitsPerSecond),
    latency: summary(bucket.metrics.latencyMilliseconds),
    jitter: summary(bucket.metrics.jitterMilliseconds),
    packetLoss: summary(bucket.metrics.packetLossPercent),
    duration: summary(bucket.metrics.executionDurationMilliseconds),
  }
}

function normalizeProvider(value: Provider | RawProviderDescriptor): Provider {
  const nested = 'availability' in value ? value.availability : undefined
  return {
    id: value.id,
    displayName: value.displayName,
    capabilities: value.capabilities,
    available: 'available' in value && typeof value.available === 'boolean' ? value.available : nested?.available ?? false,
    version: 'version' in value && typeof value.version === 'string' ? value.version : nested?.version ?? '',
    message: 'message' in value && typeof value.message === 'string' ? value.message : nested?.message ?? '',
  }
}

async function requestValidation(path: string, input?: TaskInput): Promise<RouteValidation> {
  const headers = new Headers({ Accept: 'application/json' })
  if (input !== undefined) headers.set('Content-Type', 'application/json')
  const response = await fetch(`${API_ROOT}${path}`, { method: 'POST', headers, ...(input !== undefined ? { body: JSON.stringify(input) } : {}), credentials: 'same-origin' })
  if (response.ok || response.status === 422) {
    const payload = await response.json() as RouteValidation | ApiErrorEnvelope
    if ('success' in payload) return payload
    throw parseErrorFromPayload(response, payload)
  }
  throw await parseError(response)
}

function parseErrorFromPayload(response: Response, payload: RouteValidation | ApiErrorEnvelope): ApiError {
  if ('error' in payload) return new ApiError(payload.error.message, response.status, payload.error.code, payload.error.requestId, payload.error.details)
  return new ApiError(response.statusText || 'Validation failed.', response.status)
}

async function downloadBackup(): Promise<{ blob: Blob; filename: string }> {
  const response = await fetch(`${API_ROOT}/backup`, { method: 'POST', headers: { Accept: 'application/vnd.sqlite3' }, credentials: 'same-origin' })
  if (!response.ok) throw await parseError(response)
  const disposition = response.headers.get('Content-Disposition') ?? ''
  const match = /filename="?([^";]+)"?/i.exec(disposition)
  return { blob: await response.blob(), filename: match?.[1] ?? 'multispeed-backup.db' }
}

async function downloadConfiguration(): Promise<{ blob: Blob; filename: string }> {
  const response = await fetch(`${API_ROOT}/config/export`, { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
  if (!response.ok) throw await parseError(response)
  const disposition = response.headers.get('Content-Disposition') ?? ''
  const match = /filename="?([^";]+)"?/i.exec(disposition)
  return { blob: await response.blob(), filename: match?.[1] ?? 'multispeed-config.json' }
}

export function getApiErrorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return 'An unexpected error occurred.'
}
