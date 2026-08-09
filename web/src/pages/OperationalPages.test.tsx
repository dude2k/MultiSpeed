import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { fallbackSettings } from '../hooks/useAppSettings'
import { api } from '../lib/api'
import type { ConfigurationDocument, Provider, SystemInfo } from '../lib/types'
import { renderPage } from '../test/render'
import NetworkPage from './NetworkPage'
import SettingsPage from './SettingsPage'
import SystemPage from './SystemPage'

const capabilities = { serverDiscovery: true, fixedServerIds: true, customServerUrls: false, interfaceBinding: true, sourceAddressBinding: true, ipv4: true, ipv6: true, jitter: true, packetLoss: false, resultUrls: false }
const provider: Provider = { id: 'librespeed', displayName: 'LibreSpeed', available: false, version: '', message: 'Binary unavailable', capabilities }
const system: SystemInfo = {
  version: '1.2.3', gitCommit: 'abcdef0', buildDate: '2026-08-05T00:00:00Z', goVersion: 'go1.26.5', operatingSystem: 'linux', architecture: 'amd64', databasePath: '/data/multispeed.db', databaseSizeBytes: 2048, schemaVersion: 5, uptimeSeconds: 7200, taskCount: 3, resultCount: 42, runningTaskCount: 1,
  providers: [provider], interfaces: [{ name: 'eth0', index: 2, operational: true, loopback: false, virtual: false, macAddress: '', mtu: 1500, addresses: [{ address: '192.0.2.10', family: 'ipv4', linkLocal: false }] }],
}

describe('network, settings, and system operations pages', () => {
  it('shows actionable network empty states and refetches virtual interfaces on demand', async () => {
    const interfaces = vi.spyOn(api, 'interfaces').mockResolvedValue([])
    vi.spyOn(api, 'routes').mockResolvedValue([])
    const user = userEvent.setup()
    renderPage(<NetworkPage />, { route: '/network' })
    expect(await screen.findByText('No matching interfaces')).toBeVisible()
    expect(screen.getByText('No route expectations yet')).toBeVisible()
    await user.click(screen.getByRole('switch', { name: 'Virtual' }))
    await waitFor(() => expect(interfaces).toHaveBeenLastCalledWith({ includeDown: false, includeVirtual: true }))
  })

  it('aligns settings controls with the API constraints and supports broad IANA timezone selection', async () => {
    vi.spyOn(api, 'settings').mockResolvedValue(fallbackSettings)
    const update = vi.spyOn(api, 'updateSettings').mockImplementation((value) => Promise.resolve(value))
    const user = userEvent.setup()
    renderPage(<SettingsPage />, { route: '/settings' })
    expect(await screen.findByText('Display & task defaults')).toBeVisible()
    const timeout = document.querySelector<HTMLInputElement>('input[name="defaultTaskTimeoutSeconds"]')
    const refresh = document.querySelector<HTMLInputElement>('input[name="interfaceRefreshIntervalSeconds"]')
    const timezone = document.querySelector<HTMLSelectElement>('select[name="defaultTimezone"]')
    expect(timeout).toHaveAttribute('min', '5')
    expect(timeout).toHaveAttribute('max', '3600')
    expect(refresh).toHaveAttribute('min', '5')
    expect(refresh).toHaveAttribute('max', '3600')
    await user.selectOptions(timezone as HTMLSelectElement, 'Asia/Kolkata')
    await user.click(screen.getAllByRole('button', { name: /Save settings/i })[0] as HTMLElement)
    await waitFor(() => expect(update.mock.calls[0]?.[0]).toMatchObject({ defaultTimezone: 'Asia/Kolkata' }))
  })

  it('requires explicit confirmation before recording Ookla EULA acceptance', async () => {
    vi.spyOn(api, 'settings').mockResolvedValue(fallbackSettings)
    const update = vi.spyOn(api, 'updateOoklaEula').mockResolvedValue({
      ...fallbackSettings,
      ooklaEulaAccepted: true,
      ooklaEulaAcceptedAt: '2026-08-05T15:00:00Z',
      ooklaEulaVersion: 'speedtest-eula-reviewed-2026-08-07',
      ooklaEulaCurrentVersion: 'speedtest-eula-reviewed-2026-08-07',
      ooklaEulaEffectiveAccepted: true,
      ooklaEulaAcceptanceSource: 'persisted',
    })
    const user = userEvent.setup()
    renderPage(<SettingsPage />, { route: '/settings' })

    const terms = await screen.findByRole('link', { name: /Review current Ookla EULA/i })
    expect(terms).toHaveAttribute('href', 'https://www.speedtest.net/about/eula')
    expect(terms).toHaveAttribute('rel', expect.stringContaining('noopener'))
    const record = screen.getByRole('button', { name: /Record acceptance/i })
    expect(record).toBeDisabled()

    await user.click(screen.getByRole('switch', { name: /I reviewed and accept the current Ookla EULA/i }))
    await user.click(record)
    await waitFor(() => expect(update).toHaveBeenCalledWith(true, true))
    expect(await screen.findByText('Acceptance recorded')).toBeVisible()
  })

  it('shows an environment EULA override without offering a misleading revoke action', async () => {
    vi.spyOn(api, 'settings').mockResolvedValue({
      ...fallbackSettings,
      ooklaEulaCurrentVersion: 'speedtest-eula-reviewed-2026-08-07',
      ooklaEulaEffectiveAccepted: true,
      ooklaEulaAcceptanceSource: 'environment',
    })
    renderPage(<SettingsPage />, { route: '/settings' })
    expect(await screen.findByText('Accepted by environment')).toBeVisible()
    expect(screen.getByText('ACCEPT_OOKLA_EULA=true')).toBeVisible()
    expect(screen.queryByRole('button', { name: /Revoke acceptance/i })).not.toBeInTheDocument()
  })

  it('previews and confirms a configuration import before replacing saved values', async () => {
    vi.spyOn(api, 'settings').mockResolvedValue(fallbackSettings)
    const importConfiguration = vi.spyOn(api, 'importConfiguration').mockResolvedValue({ importedAt: '2026-08-07T18:01:00Z', taskCount: 2, routeProfileCount: 1, settingsUpdated: true })
    const configuration = {
      format: 'multispeed-config', version: 1, exportedAt: '2026-08-07T18:00:00Z', applicationVersion: '1.0.0',
      settings: { displayUnits: 'bits', defaultTimezone: 'UTC', globalConcurrency: 1, allowSeparateWanConcurrency: false, retentionMode: 'forever', retentionValue: 0, defaultChartRange: '30d', interfaceRefreshIntervalSeconds: 30, defaultTaskTimeoutSeconds: 120, databaseMaintenanceSchedule: '0 3 * * 0' },
      routeProfiles: [{ id: '0b982166-e6b2-44ea-9602-9db18c21ab54', name: 'WAN', description: '', interfaceName: 'eth0', sourceIp: '192.0.2.10', expectedGateway: '', expectedRoutingTable: '', validationTarget: '1.1.1.1', notes: '' }],
      tasks: [
        { id: '26f8bdda-8ceb-4220-8b2f-c91d22b681f8', name: 'One', description: '', enabled: false, provider: 'cloudflare', cronExpression: '0 * * * *', timezone: 'UTC', randomJitterSeconds: 0, serverSelectionMode: 'automatic', serverId: '', serverUrl: '', customServerDefinition: {}, interfaceName: 'eth0', sourceIp: '192.0.2.10', ipFamily: 'ipv4', routeProfileId: null, timeoutSeconds: 120, providerOptions: {}, preventOverlap: true, routeValidation: 'required' },
        { id: 'c0621911-b885-42fa-96df-0874bfdebc60', name: 'Two', description: '', enabled: false, provider: 'librespeed', cronExpression: '30 * * * *', timezone: 'UTC', randomJitterSeconds: 0, serverSelectionMode: 'automatic', serverId: '', serverUrl: '', customServerDefinition: {}, interfaceName: 'eth0', sourceIp: '192.0.2.10', ipFamily: 'ipv4', routeProfileId: '0b982166-e6b2-44ea-9602-9db18c21ab54', timeoutSeconds: 120, providerOptions: {}, preventOverlap: true, routeValidation: 'required' },
      ],
    } satisfies ConfigurationDocument
    const user = userEvent.setup()
    renderPage(<SettingsPage />, { route: '/settings' })
    await screen.findByText('Configuration import & export')

    const file = new File([JSON.stringify(configuration)], 'saved-config.json', { type: 'application/json' })
    await user.upload(screen.getByLabelText('Configuration file'), file)

    expect(await screen.findByRole('alertdialog')).toHaveTextContent('saved-config.json contains 2 tasks and 1 route profiles')
    expect(importConfiguration).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Import configuration' }))
    await waitFor(() => expect(importConfiguration).toHaveBeenCalledWith(configuration))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
  })

  it('renders runtime, provider, and interface facts without exposing arbitrary data', async () => {
    vi.spyOn(api, 'system').mockResolvedValue(system)
    vi.spyOn(api, 'health').mockResolvedValue({ status: 'healthy' })
    renderPage(<SystemPage />, { route: '/system' })
    expect(await screen.findByText('MultiSpeed 1.2.3')).toBeVisible()
    expect(screen.getByText('Binary unavailable')).toBeVisible()
    expect(screen.getByText('linux/amd64')).toBeVisible()
    expect(screen.getByText('192.0.2.10')).toBeVisible()
  })

  it('renders an interface without addresses instead of crashing the system page', async () => {
    vi.spyOn(api, 'system').mockResolvedValue({
      ...system,
      interfaces: [{ ...system.interfaces[0], addresses: null }],
    } as unknown as SystemInfo)
    vi.spyOn(api, 'health').mockResolvedValue({ status: 'healthy' })
    renderPage(<SystemPage />, { route: '/system' })
    expect(await screen.findByText('Interface snapshot')).toBeVisible()
    expect(screen.getByText('eth0')).toBeVisible()
  })

  it('offers a retry when system facts cannot be loaded', async () => {
    vi.spyOn(api, 'system').mockRejectedValue(new Error('System endpoint unavailable'))
    vi.spyOn(api, 'health').mockResolvedValue({ status: 'healthy' })
    renderPage(<SystemPage />, { route: '/system' })
    expect(await screen.findByRole('alert')).toHaveTextContent('System endpoint unavailable')
    expect(screen.getByRole('button', { name: /Try again/i })).toBeVisible()
  })
})
