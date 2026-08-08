import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import { fallbackSettings } from '../hooks/useAppSettings'
import type { Result, Task } from '../lib/types'
import { renderPage } from '../test/render'
import DashboardPage from './DashboardPage'

const task: Task = { id: 'task-1', name: 'Fiber WAN', description: '', enabled: true, provider: 'cloudflare', cronExpression: '0 * * * *', timezone: 'UTC', randomJitterSeconds: 0, serverSelectionMode: 'automatic', serverId: '', serverUrl: '', customServerDefinition: {}, interfaceName: 'eth0', sourceIp: '192.0.2.10', ipFamily: 'auto', routeProfileId: null, timeoutSeconds: 120, providerOptions: {}, preventOverlap: true, routeValidation: 'required', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', lastScheduledAt: null, nextScheduledAt: '2026-12-01T00:00:00Z' }
const neverRunTask: Task = { ...task, id: 'task-2', name: 'Backup WAN', enabled: false, interfaceName: 'eth1', sourceIp: '198.51.100.10', nextScheduledAt: null }
const running: Result = { id: 'result-1', taskId: task.id, routeProfileId: null, trigger: 'manual', provider: 'cloudflare', scheduledAt: null, startedAt: '2026-01-01T00:00:00Z', finishedAt: null, status: 'running', downloadBitsPerSecond: null, uploadBitsPerSecond: null, latencyMilliseconds: null, jitterMilliseconds: null, packetLossPercent: null, downloadBytes: null, uploadBytes: null, selectedInterface: 'eth0', selectedSourceIp: '192.0.2.10', detectedPublicIp: '', serverId: '', serverName: '', serverHost: '', serverSponsor: '', serverLocation: '', serverCountry: '', providerResultUrl: '', cloudflareColo: '', routeValidationSnapshot: {}, executionDurationMs: 0, processExitCode: null, sanitizedError: '', rawProviderResponse: '', providerVersion: '', applicationVersion: '', tlsVerificationDisabled: false }

describe('dashboard states', () => {
  it('presents running tests and interface availability', async () => {
    vi.spyOn(api, 'tasks').mockResolvedValue([task, neverRunTask])
    vi.spyOn(api, 'dashboardSummary').mockResolvedValue({ latestByTask: [{ taskId: task.id, taskName: task.name, enabled: true, interfaceName: task.interfaceName, sourceIp: task.sourceIp, latestResult: running }, { taskId: neverRunTask.id, taskName: neverRunTask.name, enabled: false, interfaceName: neverRunTask.interfaceName, sourceIp: neverRunTask.sourceIp, latestResult: null }], latestByPath: [], activeRuns: [running], recentFailures: [], failedTaskCount: 0 })
    vi.spyOn(api, 'statistics').mockResolvedValue({ buckets: [], series: [] })
    vi.spyOn(api, 'settings').mockResolvedValue(fallbackSettings)
    vi.spyOn(api, 'interfaces').mockResolvedValue([{ name: 'eth0', index: 2, operational: true, loopback: false, virtual: false, macAddress: '', mtu: 1500, addresses: [{ address: '192.0.2.10', family: 'ipv4', linkLocal: false }] }])
    renderPage(<DashboardPage />)
    expect(await screen.findByText('1 test active')).toBeInTheDocument()
    expect(screen.getByText('Network readiness')).toBeInTheDocument()
    expect(screen.getAllByText('Fiber WAN').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/running/i).length).toBeGreaterThan(0)
    expect(screen.getByText('Never run')).toBeVisible()
  })

  it('renders an actionable error state', async () => {
    vi.spyOn(api, 'tasks').mockRejectedValue(new Error('Backend unavailable'))
    vi.spyOn(api, 'dashboardSummary').mockResolvedValue({ latestByTask: [], latestByPath: [], activeRuns: [], recentFailures: [], failedTaskCount: 0 })
    vi.spyOn(api, 'statistics').mockResolvedValue({ buckets: [], series: [] })
    vi.spyOn(api, 'settings').mockResolvedValue(fallbackSettings)
    vi.spyOn(api, 'interfaces').mockResolvedValue([])
    renderPage(<DashboardPage />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Backend unavailable')
    expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
  })

  it('keeps operations visible when only the throughput range exceeds its safety limit', async () => {
    vi.spyOn(api, 'tasks').mockResolvedValue([task])
    vi.spyOn(api, 'dashboardSummary').mockResolvedValue({ latestByTask: [], latestByPath: [], activeRuns: [], recentFailures: [], failedTaskCount: 0 })
    vi.spyOn(api, 'statistics').mockRejectedValue(new Error('Use a coarser granularity or shorter range.'))
    vi.spyOn(api, 'settings').mockResolvedValue(fallbackSettings)
    vi.spyOn(api, 'interfaces').mockResolvedValue([{ name: 'eth0', index: 2, operational: true, loopback: false, virtual: false, macAddress: '', mtu: 1500, addresses: [] }])
    renderPage(<DashboardPage />)
    expect(await screen.findByText('Every WAN, one operational picture.')).toBeVisible()
    expect(screen.getByText('Network readiness')).toBeVisible()
    expect(screen.getByRole('alert')).toHaveTextContent('Use a coarser granularity or shorter range.')
    expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
  })
})
