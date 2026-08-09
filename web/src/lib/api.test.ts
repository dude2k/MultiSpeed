import { describe, expect, it, vi } from 'vitest'
import { api } from './api'
import type { ConfigurationDocument, TaskInput } from './types'

const capabilities = {
  serverDiscovery: true,
  fixedServerIds: true,
  customServerUrls: true,
  interfaceBinding: true,
  sourceAddressBinding: true,
  ipv4: true,
  ipv6: true,
  jitter: true,
  packetLoss: true,
  resultUrls: true,
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function metric(value: number, count = 2) {
  return { count, minimum: value * .8, maximum: value * 1.2, average: value, median: value * .95, p95: value * 1.15, standardDeviation: value * .1 }
}

function statisticsBucket() {
  return {
    start: '2026-08-01T00:00:00.000Z', end: '2026-08-02T00:00:00.000Z', label: '01 Aug',
    counts: { total: 2, successful: 1, failed: 1, skipped: 0, cancelled: 0, other: 0 }, successRatePercent: 50, failureRatePercent: 50,
    metrics: { downloadBitsPerSecond: metric(500_000_000, 1), uploadBitsPerSecond: metric(100_000_000, 1), latencyMilliseconds: metric(15, 1), jitterMilliseconds: metric(2, 1), packetLossPercent: metric(0, 1), executionDurationMilliseconds: metric(12_000, 1) },
  }
}

describe('API contract adapters', () => {
  it('uses canonical statistic query names and normalizes a report bucket', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse({
      from: '2026-08-01T00:00:00.000Z',
      to: '2026-08-02T00:00:00.000Z',
      granularity: 'day',
      reportingTimezone: 'Europe/Berlin',
      groupBy: 'interface',
      totalResults: 2,
      overall: statisticsBucket(),
      groups: [{
        key: 'eth0',
        label: 'eth0',
        overall: statisticsBucket(),
        buckets: [statisticsBucket()],
      }],
    }))
    vi.stubGlobal('fetch', fetchMock)

    const report = await api.statistics({
      granularity: 'day',
      metric: 'download',
      from: '2026-08-01T00:00:00.000Z',
      to: '2026-08-02T00:00:00.000Z',
      timezone: 'Europe/Berlin',
      taskId: ['task-1'],
      interfaceName: ['eth0'],
      provider: ['cloudflare'],
      serverId: [],
      groupBy: 'interface',
    })

    const firstCall = fetchMock.mock.calls[0]
    const requestUrl = firstCall?.[0]
    expect(typeof requestUrl).toBe('string')
    const url = new URL(typeof requestUrl === 'string' ? requestUrl : '', 'https://multispeed.test')
    expect(url.pathname).toBe('/api/v1/statistics')
    expect(url.searchParams.get('granularity')).toBe('day')
    expect(url.searchParams.getAll('interface')).toEqual(['eth0'])
    expect(url.searchParams.get('groupBy')).toBe('interface')
    expect(url.searchParams.has('interfaceName')).toBe(false)
    expect(url.searchParams.has('metric')).toBe(false)
    expect(report.timezone).toBe('Europe/Berlin')
    expect(report.series?.[0]?.overall).toMatchObject({ sampleCount: 2, successfulCount: 1, failedCount: 1 })
    expect(report.overall).toMatchObject({ sampleCount: 2, successfulCount: 1, failedCount: 1 })
    expect(report.buckets[0]).toMatchObject({
      bucket: '2026-08-01T00:00:00.000Z',
      label: 'eth0 / 01 Aug',
      sampleCount: 2,
      successfulCount: 1,
      failedCount: 1,
      successRate: 50,
      download: { min: 400_000_000, max: 600_000_000, average: 500_000_000 },
    })
  })

  it('normalizes nested provider availability and the legacy system build timestamp', async () => {
    const provider = { id: 'librespeed', displayName: 'LibreSpeed', capabilities, availability: { available: true, version: '1.0.12', message: 'Ready' } }
    const fetchMock = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ items: [provider] }))
      .mockResolvedValueOnce(jsonResponse({
        version: '1.0.0', gitCommit: 'abcdef0', buildTime: '2026-08-05T07:00:00Z', goVersion: 'go1.25', operatingSystem: 'linux', architecture: 'amd64',
        databasePath: '/data/multispeed.db', databaseSizeBytes: 1024, schemaVersion: 1, uptimeSeconds: 60, taskCount: 1, resultCount: 2, runningTaskCount: 0,
        providers: [provider], interfaces: [{
          name: 'eth0', index: 2, operational: true, loopback: false, virtual: false,
          macAddress: '', mtu: 1500, addresses: null,
        }],
      }))
    vi.stubGlobal('fetch', fetchMock)

    const providers = await api.providers()
    const system = await api.system()

    expect(providers[0]).toMatchObject({ available: true, version: '1.0.12', message: 'Ready' })
    expect(system.buildDate).toBe('2026-08-05T07:00:00Z')
    expect(system.providers[0]).toMatchObject({ available: true, version: '1.0.12', message: 'Ready' })
    expect(system.interfaces[0]?.addresses).toEqual([])
  })

  it('downloads the SQLite backup response as a named blob', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response('SQLite format 3', {
      status: 200,
      headers: { 'Content-Type': 'application/vnd.sqlite3', 'Content-Disposition': 'attachment; filename="multispeed-20260805.db"' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    const backup = await api.backup()

    expect(backup.filename).toBe('multispeed-20260805.db')
    expect(backup.blob.type).toBe('application/vnd.sqlite3')
    expect(backup.blob.size).toBe(15)
  })

  it('downloads and imports the versioned configuration document', async () => {
    const configuration = {
      format: 'multispeed-config', version: 1, exportedAt: '2026-08-07T18:00:00Z', applicationVersion: '1.0.0',
      settings: { displayUnits: 'bits', defaultTimezone: 'UTC', globalConcurrency: 1, allowSeparateWanConcurrency: false, retentionMode: 'forever', retentionValue: 0, defaultChartRange: '30d', interfaceRefreshIntervalSeconds: 30, defaultTaskTimeoutSeconds: 120, databaseMaintenanceSchedule: '0 3 * * 0' },
      routeProfiles: [], tasks: [],
    } satisfies ConfigurationDocument
    const fetchMock = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(new Response(JSON.stringify(configuration), { status: 200, headers: { 'Content-Type': 'application/json', 'Content-Disposition': 'attachment; filename="multispeed-config-20260807T180000Z.json"' } }))
      .mockResolvedValueOnce(jsonResponse({ importedAt: '2026-08-07T18:01:00Z', taskCount: 0, routeProfileCount: 0, settingsUpdated: true }))
    vi.stubGlobal('fetch', fetchMock)

    const exported = await api.exportConfiguration()
    const imported = await api.importConfiguration(configuration)

    expect(exported.filename).toBe('multispeed-config-20260807T180000Z.json')
    expect(exported.blob.type).toBe('application/json')
    expect(imported).toMatchObject({ taskCount: 0, routeProfileCount: 0, settingsUpdated: true })
    expect(fetchMock.mock.calls[1]).toEqual([
      '/api/v1/config/import',
      expect.objectContaining({ method: 'POST', body: JSON.stringify(configuration), credentials: 'same-origin' }),
    ])
  })

  it('posts the complete unsaved task candidate and preserves a 422 validation snapshot', async () => {
    const input = { name: 'Candidate', description: '', enabled: true, provider: 'cloudflare', cronExpression: '0 * * * *', timezone: 'UTC', randomJitterSeconds: 0, serverSelectionMode: 'automatic', serverId: '', serverUrl: '', customServerDefinition: {}, interfaceName: 'eth0', sourceIp: '192.0.2.10', ipFamily: 'ipv4', routeProfileId: null, timeoutSeconds: 120, providerOptions: {}, preventOverlap: true, routeValidation: 'required' } satisfies TaskInput
    const validation = { success: false, interfaceName: 'eth0', sourceIp: '192.0.2.10', gateway: '', routingTable: '', destination: 'one.one.one.one', detectedPublicIp: '', reachable: false, durationMs: 3, validatedAt: '2026-08-05T00:00:00Z', message: 'source route mismatch' }
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(validation), { status: 422, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    await expect(api.validateTaskInput(input)).resolves.toEqual(validation)
    const [url, init] = fetchMock.mock.calls[0] ?? []
    expect(url).toBe('/api/v1/tasks/validate')
    expect(init).toMatchObject({ method: 'POST', body: JSON.stringify(input), credentials: 'same-origin' })
  })
})
