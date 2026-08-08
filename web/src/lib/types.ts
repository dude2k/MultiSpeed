export type ProviderId = 'ookla' | 'librespeed' | 'cloudflare'
export type ResultStatus = 'queued' | 'validating' | 'running' | 'succeeded' | 'failed' | 'skipped' | 'cancelled'
export type ThemeMode = 'light' | 'dark' | 'system'

export interface Task {
  id: string
  name: string
  description: string
  enabled: boolean
  provider: ProviderId
  cronExpression: string
  timezone: string
  randomJitterSeconds: number
  serverSelectionMode: 'automatic' | 'fixed' | 'custom'
  serverId: string
  serverUrl: string
  customServerDefinition: Record<string, unknown>
  interfaceName: string
  sourceIp: string
  ipFamily: 'auto' | 'ipv4' | 'ipv6'
  routeProfileId: string | null
  timeoutSeconds: number
  providerOptions: Record<string, unknown>
  preventOverlap: boolean
  routeValidation: 'required' | 'interface-only'
  createdAt: string
  updatedAt: string
  lastScheduledAt: string | null
  nextScheduledAt: string | null
  networkPathValid?: boolean
  networkPathMessage?: string
}

export type TaskInput = Omit<Task, 'id' | 'createdAt' | 'updatedAt' | 'lastScheduledAt' | 'nextScheduledAt' | 'networkPathValid' | 'networkPathMessage'>

export interface Result {
  id: string
  taskId: string
  routeProfileId: string | null
  trigger: 'manual' | 'scheduled'
  provider: ProviderId
  scheduledAt: string | null
  startedAt: string | null
  finishedAt: string | null
  status: ResultStatus
  downloadBitsPerSecond: number | null
  uploadBitsPerSecond: number | null
  latencyMilliseconds: number | null
  jitterMilliseconds: number | null
  packetLossPercent: number | null
  downloadBytes: number | null
  uploadBytes: number | null
  selectedInterface: string
  selectedSourceIp: string
  detectedPublicIp: string
  serverId: string
  serverName: string
  serverHost: string
  serverSponsor: string
  serverLocation: string
  serverCountry: string
  providerResultUrl: string
  cloudflareColo: string
  routeValidationSnapshot?: Record<string, unknown>
  executionDurationMs: number
  processExitCode: number | null
  sanitizedError: string
  rawProviderResponse?: string
  providerVersion: string
  applicationVersion: string
  tlsVerificationDisabled: boolean
}

export type ResultSummary = Omit<Result, 'rawProviderResponse' | 'routeValidationSnapshot'>

export interface DashboardTaskSummary {
  taskId: string
  taskName: string
  enabled: boolean
  interfaceName: string
  sourceIp: string
  latestResult: ResultSummary | null
}

export interface DashboardPathSummary {
  interfaceName: string
  sourceIp: string
  taskIds: string[]
  latestResult: ResultSummary | null
}

export interface DashboardSummary {
  latestByTask: DashboardTaskSummary[]
  latestByPath: DashboardPathSummary[]
  activeRuns: ResultSummary[]
  recentFailures: ResultSummary[]
  failedTaskCount: number
}

export interface Page<T> {
  items: T[]
  page: number
  pageSize: number
  totalItems: number
  totalPages: number
}

export interface InterfaceAddress {
  address: string
  family: 'ipv4' | 'ipv6'
  linkLocal: boolean
}

export interface NetworkInterface {
  name: string
  index: number
  operational: boolean
  loopback: boolean
  virtual: boolean
  macAddress: string
  mtu: number
  addresses: InterfaceAddress[]
  operationalState?: string
}

export interface RouteValidation {
  success: boolean
  interfaceName: string
  sourceIp: string
  gateway: string
  routingTable: string
  destination: string
  detectedPublicIp: string
  reachable: boolean
  durationMs: number
  validatedAt: string
  message: string
}

export interface RouteProfile {
  id: string
  name: string
  description: string
  interfaceName: string
  sourceIp: string
  expectedGateway: string
  expectedRoutingTable: string
  validationTarget: string
  notes: string
  createdAt: string
  updatedAt: string
  lastValidationAt: string | null
  lastValidationSucceeded: boolean | null
  lastValidationSnapshot: Partial<RouteValidation> & Record<string, unknown>
}

export type RouteProfileInput = Omit<RouteProfile, 'id' | 'createdAt' | 'updatedAt' | 'lastValidationAt' | 'lastValidationSucceeded' | 'lastValidationSnapshot'>

export interface ProviderCapabilities {
  serverDiscovery: boolean
  fixedServerIds: boolean
  customServerUrls: boolean
  interfaceBinding: boolean
  sourceAddressBinding: boolean
  ipv4: boolean
  ipv6: boolean
  jitter: boolean
  packetLoss: boolean
  resultUrls: boolean
}

export interface Provider {
  id: ProviderId
  displayName: string
  available: boolean
  version: string
  message: string
  capabilities: ProviderCapabilities
}

export interface ProviderServer {
  id: string
  name: string
  sponsor: string
  host: string
  location: string
  country: string
  distanceKilometers?: number
}

export interface Settings {
  displayUnits: 'bits' | 'bytes'
  defaultTimezone: string
  globalConcurrency: number
  allowSeparateWanConcurrency: boolean
  retentionMode: 'forever' | 'days' | 'months'
  retentionValue: number
  defaultChartRange: string
  interfaceRefreshIntervalSeconds: number
  defaultTaskTimeoutSeconds: number
  databaseMaintenanceSchedule: string
  ooklaEulaAccepted: boolean
  ooklaEulaAcceptedAt: string | null
  ooklaEulaVersion: string
  ooklaEulaCurrentVersion: string
  ooklaEulaEffectiveAccepted: boolean
  ooklaEulaAcceptanceSource: 'none' | 'persisted' | 'environment'
}

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

export interface ConfigurationDocument {
  format: 'multispeed-config'
  version: 1
  exportedAt: string
  applicationVersion: string
  settings: ConfigurationSettings
  routeProfiles: ConfigurationRouteProfile[]
  tasks: ConfigurationTask[]
}

export interface ConfigurationImportResult {
  importedAt: string
  taskCount: number
  routeProfileCount: number
  settingsUpdated: boolean
}

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
