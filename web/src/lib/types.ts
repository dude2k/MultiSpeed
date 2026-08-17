import type * as ApiContract from './openapi'

export type ProviderId = ApiContract.ProviderId
export type ResultStatus = ApiContract.ResultStatus
export type ThemeMode = 'light' | 'dark' | 'system'

// Persisted task responses contain the server-applied defaults even where the
// OpenAPI input schema permits callers to omit them.
export type Task = Required<Omit<ApiContract.Task, 'networkPathValid' | 'networkPathMessage'>> &
  Partial<Pick<ApiContract.Task, 'networkPathValid' | 'networkPathMessage'>>

export type TaskInput = Omit<Task, 'id' | 'createdAt' | 'updatedAt' | 'lastScheduledAt' | 'nextScheduledAt' | 'networkPathValid' | 'networkPathMessage'>

export type Result = Required<Omit<ApiContract.ResultSummary, 'queuedAt'>> &
  Partial<Pick<ApiContract.ResultSummary, 'queuedAt'>> &
  Partial<Pick<ApiContract.Result, 'rawProviderResponse' | 'routeValidationSnapshot'>>

export type ResultSummary = Omit<Result, 'rawProviderResponse' | 'routeValidationSnapshot'>

export type DashboardTaskSummary = Omit<ApiContract.DashboardTaskResult, 'latestResult'> & { latestResult: ResultSummary | null }
export type DashboardPathSummary = Omit<ApiContract.DashboardPathResult, 'latestResult'> & { latestResult: ResultSummary | null }
export type DashboardSummary = Omit<ApiContract.DashboardResultSummary, 'latestByTask' | 'latestByPath' | 'activeRuns' | 'recentFailures'> & {
  latestByTask: DashboardTaskSummary[]
  latestByPath: DashboardPathSummary[]
  activeRuns: ResultSummary[]
  recentFailures: ResultSummary[]
}

export interface Page<T> {
  items: T[]
  page: number
  pageSize: number
  totalItems: number
  totalPages: number
}

export type InterfaceAddress = ApiContract.InterfaceAddress
export type NetworkInterface = Omit<ApiContract.NetworkInterface, 'operationalState'> & { operationalState?: string }
export type RouteValidation = ApiContract.RouteValidation
export type RouteProfile = Omit<Required<ApiContract.RouteProfile>, 'lastValidationSnapshot'> & {
  lastValidationSnapshot: Partial<RouteValidation> & Record<string, unknown>
}

export type RouteProfileInput = Omit<RouteProfile, 'id' | 'createdAt' | 'updatedAt' | 'lastValidationAt' | 'lastValidationSucceeded' | 'lastValidationSnapshot'>

export type ProviderCapabilities = ApiContract.ProviderCapabilities
export type Provider = ApiContract.ProviderDescriptor
export type OoklaBinaryStatus = ApiContract.OoklaBinaryStatus
export type OoklaBinaryInstallResult = ApiContract.OoklaBinaryInstallResult
export type ProviderServer = ApiContract.ProviderServer
export type Settings = Required<ApiContract.Settings>

export type ConfigurationSettings = Pick<Settings,
  | 'displayUnits'
  | 'defaultTimezone'
  | 'globalConcurrency'
  | 'allowSeparateWanConcurrency'
  | 'retentionMode'
  | 'retentionValue'
  | 'defaultChartRange'
  | 'interfaceRefreshIntervalSeconds'
  | 'defaultTaskTimeoutSeconds'
  | 'databaseMaintenanceSchedule'
>

export type ConfigurationTask = TaskInput & { id: string }
export type ConfigurationRouteProfile = RouteProfileInput & { id: string }

export type ConfigurationDocument = ApiContract.ConfigurationDocument
export type ConfigurationImportResult = ApiContract.ConfigurationImportResult

export interface StatisticBucket {
  bucket: string
  resultId?: string
  timestamp?: string
  label?: string
  sampleCount: number
  successfulCount: number
  failedCount: number
  skippedCount: number
  successRate?: number
  download?: MetricAggregate
  upload?: MetricAggregate
  latency?: MetricAggregate
  jitter?: MetricAggregate
  packetLoss?: MetricAggregate
  duration?: MetricAggregate
  [key: string]: unknown
}

export interface MetricAggregate {
  min: number | null
  max: number | null
  average: number | null
  median: number | null
  p95: number | null
  standardDeviation: number | null
}

export interface StatisticsResponse {
  buckets: StatisticBucket[]
  overall?: StatisticBucket
  totals?: StatisticBucket
  summary?: StatisticBucket
  series?: Array<{ id: string; name: string; overall?: StatisticBucket; buckets: StatisticBucket[] }>
  timezone?: string
  groupBy?: string
}

export interface SystemInfo {
  version: string
  gitCommit: string
  buildDate: string
  buildTime?: string
  goVersion: string
  operatingSystem: string
  architecture: string
  databasePath: string
  databaseSizeBytes: number
  schemaVersion: string | number
  uptimeSeconds: number
  taskCount: number
  resultCount: number
  runningTaskCount: number
  providers: Provider[]
  interfaces: NetworkInterface[]
}

export interface ApiErrorEnvelope {
  error: {
    code: string
    message: string
    requestId?: string
    details?: Record<string, string[]>
  }
}

export interface LiveEvent {
  id?: string
  type: string
  timestamp?: string
  taskId?: string
  resultId?: string
  payload?: Record<string, unknown>
  data?: unknown
}
