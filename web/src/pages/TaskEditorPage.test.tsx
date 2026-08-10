import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import { fallbackSettings } from '../hooks/useAppSettings'
import type { Provider, Task } from '../lib/types'
import { renderPage } from '../test/render'
import TaskEditorPage, { buildTaskInput, customEndpointSchema, taskSchema, type TaskFormValues } from './TaskEditorPage'

const capabilities = { serverDiscovery: true, fixedServerIds: true, customServerUrls: true, interfaceBinding: true, sourceAddressBinding: true, ipv4: true, ipv6: true, jitter: true, packetLoss: true, resultUrls: true }
const providers: Provider[] = [
  { id: 'ookla', displayName: 'Ookla Speedtest', available: false, version: '', message: 'Accept the Ookla EULA in Settings before using this provider.', capabilities },
  { id: 'librespeed', displayName: 'LibreSpeed', available: true, version: '1.0', message: '', capabilities },
  { id: 'cloudflare', displayName: 'Cloudflare', available: true, version: 'native', message: '', capabilities: { ...capabilities, serverDiscovery: false, fixedServerIds: false } },
]

function mockEditorApi() {
  vi.spyOn(api, 'interfaces').mockResolvedValue([{ name: 'eth0', index: 2, operational: true, loopback: false, virtual: false, macAddress: '00:11:22:33:44:55', mtu: 1500, addresses: [{ address: '192.0.2.10', family: 'ipv4', linkLocal: false }] }, { name: 'wg0', index: 3, operational: true, loopback: false, virtual: true, macAddress: '', mtu: 1420, addresses: [{ address: '198.51.100.10', family: 'ipv4', linkLocal: false }] }])
  vi.spyOn(api, 'routes').mockResolvedValue([{ id: 'route-1', name: 'Primary fiber', description: '', interfaceName: 'eth0', sourceIp: '192.0.2.10', expectedGateway: '192.0.2.1', expectedRoutingTable: '100', validationTarget: 'one.one.one.one', notes: '', createdAt: '', updatedAt: '', lastValidationAt: null, lastValidationSucceeded: null, lastValidationSnapshot: {} }])
  vi.spyOn(api, 'providers').mockResolvedValue(providers)
  vi.spyOn(api, 'settings').mockResolvedValue(fallbackSettings)
  vi.spyOn(api, 'providerServers').mockResolvedValue([{ id: '42', name: 'Frankfurt', sponsor: 'Example ISP', host: 'speed.example.test', location: 'Frankfurt', country: 'DE' }])
  vi.spyOn(api, 'ooklaBinaryStatus').mockResolvedValue({ uploadEnabled: true, installed: false, maxUploadBytes: 64 * 1024 * 1024, message: 'Upload an executable.' })
}

describe('task editor', () => {
  it('enforces provider-dependent task-form validation', () => {
    const base = { name: 'WAN test', description: '', enabled: true, provider: 'cloudflare', cronExpression: '0 * * * *', timezone: 'UTC', randomJitterSeconds: 0, serverSelectionMode: 'automatic', serverId: '', serverUrl: '', customServerName: '', customDownloadPath: '', customUploadPath: '', customPingPath: '', customIpPath: '', allowInsecureCustomServer: false, interfaceName: 'eth0', sourceIp: '192.0.2.10', ipFamily: 'auto', routeProfileId: '', timeoutSeconds: 120, preventOverlap: true, routeValidation: 'required', skipTlsVerification: false } as const
    expect(taskSchema.safeParse(base).success).toBe(true)
    expect(taskSchema.safeParse({ ...base, provider: 'ookla', serverSelectionMode: 'fixed' }).success).toBe(false)
    expect(taskSchema.safeParse({ ...base, provider: 'librespeed', serverSelectionMode: 'custom', serverUrl: 'not-a-url' }).success).toBe(false)
    expect(taskSchema.safeParse({ ...base, provider: 'librespeed', serverSelectionMode: 'custom', serverUrl: 'https://speed.example.test' }).success).toBe(true)
    expect(taskSchema.safeParse({ ...base, provider: 'librespeed', serverSelectionMode: 'custom', serverUrl: 'HTTPS://speed.example.test' }).success).toBe(true)
    expect(taskSchema.safeParse({ ...base, provider: 'librespeed', serverSelectionMode: 'custom', serverUrl: 'http://speed.example.test' }).success).toBe(false)
    expect(taskSchema.safeParse({ ...base, provider: 'librespeed', serverSelectionMode: 'custom', serverUrl: 'http://speed.example.test', allowInsecureCustomServer: true }).success).toBe(true)
    expect(taskSchema.safeParse({ ...base, provider: 'librespeed', serverSelectionMode: 'custom', serverUrl: 'HTTP://speed.example.test' }).success).toBe(false)
    expect(taskSchema.safeParse({ ...base, routeValidation: 'optional' }).success).toBe(false)
    expect(taskSchema.safeParse({ ...base, provider: 'cloudflare', serverSelectionMode: 'fixed', serverId: '42' }).success).toBe(false)
  })

  it.each([
    ['leading slash', '/garbage.php'],
    ['parent traversal', '../garbage.php'],
    ['nested parent traversal', 'api/../garbage.php'],
    ['encoded parent traversal', 'api/%2e%2e/garbage.php'],
    ['backslash', 'api\\garbage.php'],
    ['encoded slash', 'api%2Fgarbage.php'],
    ['encoded backslash', 'api%5cgarbage.php'],
    ['NUL control', `garbage.php\0suffix`],
    ['line-feed control', 'garbage.php\nsuffix'],
    ['carriage-return control', 'garbage.php\rsuffix'],
    ['tab control', 'garbage.php\tsuffix'],
    ['DEL control', 'garbage.php\u007fsuffix'],
    ['encoded NUL control', 'garbage.php%00suffix'],
    ['encoded line-feed control', 'garbage.php%0asuffix'],
    ['encoded carriage-return control', 'garbage.php%0Dsuffix'],
  ])('rejects a LibreSpeed endpoint containing %s', (_case, endpoint) => {
    expect(customEndpointSchema.safeParse(endpoint).success).toBe(false)
  })

  it.each([
    ['', 'optional default'],
    ['garbage.php', 'filename'],
    ['api/v1/download', 'nested path'],
    ['download.php?nocache=true', 'relative query'],
    ['files/v1.2/speed_test~download', 'safe path punctuation'],
  ])('accepts the safe LibreSpeed endpoint %s (%s)', (endpoint) => {
    expect(customEndpointSchema.safeParse(endpoint).success).toBe(true)
  })

  it('shows Cloudflare automatic edge selection and validates identity', async () => {
    mockEditorApi()
    const user = userEvent.setup()
    renderPage(<TaskEditorPage />, { route: '/tasks/new' })
    expect(await screen.findByText('Identity & measurement provider')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Enter at least 2 characters')
    await user.type(screen.getByPlaceholderText('Fiber uplink · Frankfurt'), 'Fiber baseline')
    await user.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText('Automatic edge selection')).toBeInTheDocument()
    expect(document.querySelector('select[name="interfaceName"]')).toBeInTheDocument()
    expect(document.querySelector('select[name="sourceIp"]')).toBeInTheDocument()
    expect(document.querySelector('select[name="routeProfileId"]')).toBeInTheDocument()
  })

  it('reveals LibreSpeed custom-backend and TLS controls', async () => {
    mockEditorApi()
    const user = userEvent.setup()
    renderPage(<TaskEditorPage />, { route: '/tasks/new' })
    await screen.findByText('Identity & measurement provider')
    await user.type(screen.getByPlaceholderText('Fiber uplink · Frankfurt'), 'Libre path')
    await user.click(screen.getByRole('button', { name: /LibreSpeed/i }))
    await user.click(screen.getByRole('button', { name: /continue/i }))
    await user.click(screen.getByRole('button', { name: /Custom backend/i }))
    expect(screen.getByPlaceholderText('https://speed.example.net')).toBeInTheDocument()
    expect(screen.getByText('Skip TLS certificate verification')).toBeInTheDocument()
    expect(document.querySelector('select[name="interfaceName"]')).toBeInTheDocument()
  })

  it('hides custom backends when the deployment allowlist is empty', async () => {
    mockEditorApi()
    vi.mocked(api.providers).mockResolvedValue(providers.map((provider) => provider.id === 'librespeed' ? { ...provider, capabilities: { ...provider.capabilities, customServerUrls: false } } : provider))
    const user = userEvent.setup()
    renderPage(<TaskEditorPage />, { route: '/tasks/new' })
    await screen.findByText('Identity & measurement provider')
    await user.click(screen.getByRole('button', { name: /LibreSpeed/i }))
    await user.click(screen.getByRole('button', { name: /Target & network/i }))
    expect(screen.queryByRole('button', { name: /Custom backend/i })).not.toBeInTheDocument()
    expect(screen.getByText(/Custom backends are disabled by this deployment/i)).toBeInTheDocument()
  })

  it('discovers and selects an Ookla server through the chosen source path', async () => {
    mockEditorApi()
    const user = userEvent.setup()
    renderPage(<TaskEditorPage />, { route: '/tasks/new' })
    await screen.findByText('Identity & measurement provider')
    await user.click(screen.getByRole('button', { name: /Ookla Speedtest/i }))
    await user.click(screen.getByRole('button', { name: /Target & network/i }))
    await user.click(screen.getByRole('button', { name: /Fixed server/i }))
    await selectSourcePath(user)
    await user.click(screen.getByRole('button', { name: /^Discover$/i }))
    await user.click(await screen.findByRole('option', { name: /Frankfurt/i }))
    expect(api.providerServers).toHaveBeenCalledWith('ookla', expect.objectContaining({ interfaceName: 'eth0', sourceIp: '192.0.2.10', ipFamily: 'auto' }))
    expect(screen.getByText(/Persisted server ID:/i)).toHaveTextContent('42')
  })

  it('saves an enabled Ookla task without requiring the unavailable CLI preflight', async () => {
    mockEditorApi()
    const validate = vi.spyOn(api, 'validateTaskInput')
    const create = vi.spyOn(api, 'createTask').mockImplementation((input) => Promise.resolve({
      id: 'ookla-task', ...input, description: input.description ?? '', cronExpression: input.cronExpression ?? '0 */6 * * *',
      timezone: input.timezone ?? 'UTC', randomJitterSeconds: input.randomJitterSeconds ?? 0,
      serverSelectionMode: input.serverSelectionMode ?? 'automatic', serverId: input.serverId ?? '', serverUrl: input.serverUrl ?? '',
      customServerDefinition: input.customServerDefinition ?? {}, ipFamily: input.ipFamily ?? 'auto', routeProfileId: input.routeProfileId ?? null,
      timeoutSeconds: input.timeoutSeconds ?? 120, providerOptions: input.providerOptions ?? {}, preventOverlap: input.preventOverlap ?? true,
      routeValidation: input.routeValidation ?? 'required', createdAt: '', updatedAt: '', lastScheduledAt: null, nextScheduledAt: '',
    }))
    const user = userEvent.setup()
    renderPage(<TaskEditorPage />, { route: '/tasks/new' })
    await screen.findByText('Identity & measurement provider')
    await user.type(screen.getByPlaceholderText('Fiber uplink · Frankfurt'), 'Ookla later')
    await user.click(screen.getByRole('button', { name: /Ookla Speedtest/i }))
    await user.click(screen.getByRole('button', { name: /continue/i }))
    await selectSourcePath(user)
    await user.click(screen.getByRole('button', { name: /continue/i }))
    await user.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/Ookla tasks may be saved before the operator-installed CLI is available/i)).toBeVisible()
    expect(create).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: /create task/i }))
    await waitFor(() => expect(create).toHaveBeenCalledWith(expect.objectContaining({ enabled: true, provider: 'ookla' })))
    expect(validate).not.toHaveBeenCalled()
  })

  it('uploads a separately obtained Ookla executable from the unavailable-provider notice', async () => {
    mockEditorApi()
    vi.mocked(api.settings).mockResolvedValue({
      ...fallbackSettings,
      ooklaEulaAccepted: true,
      ooklaEulaAcceptedAt: '2026-08-10T00:00:00Z',
      ooklaEulaVersion: 'speedtest-eula-reviewed-2026-08-07',
      ooklaEulaCurrentVersion: 'speedtest-eula-reviewed-2026-08-07',
      ooklaEulaEffectiveAccepted: true,
      ooklaEulaAcceptanceSource: 'persisted',
    })
    const upload = vi.spyOn(api, 'uploadOoklaBinary').mockResolvedValue({
      uploadEnabled: true, installed: true, maxUploadBytes: 64 * 1024 * 1024, message: 'Installed.',
      version: 'Speedtest by Ookla 1.2.0.84', sha256: 'a'.repeat(64),
    })
    const user = userEvent.setup()
    renderPage(<TaskEditorPage />, { route: '/tasks/new' })
    await screen.findByText('Identity & measurement provider')
    await user.click(screen.getByRole('button', { name: /Ookla Speedtest/i }))
    expect(await screen.findByRole('link', { name: /Get official Speedtest CLI/i })).toHaveAttribute('href', 'https://www.speedtest.net/apps/cli')
    expect(screen.getByText(/choose the Linux x86_64 archive/i)).toBeVisible()
    const executable = new File([new Uint8Array([0x7f, 0x45, 0x4c, 0x46])], 'speedtest', { type: 'application/octet-stream' })
    await user.upload(await screen.findByLabelText('Ookla Speedtest executable'), executable)
    await user.click(screen.getByRole('switch', { name: /This is an authorized Speedtest by Ookla executable/i }))
    await user.click(screen.getByRole('button', { name: /Install executable/i }))
    await waitFor(() => expect(upload).toHaveBeenCalledWith(executable))
    expect(await screen.findByText(/Speedtest by Ookla 1.2.0.84 is ready/i)).toBeVisible()
  })

  it('discovers LibreSpeed servers and reveals intentional virtual WAN paths', async () => {
    mockEditorApi()
    const user = userEvent.setup()
    renderPage(<TaskEditorPage />, { route: '/tasks/new' })
    await screen.findByText('Identity & measurement provider')
    await user.click(screen.getByRole('button', { name: /LibreSpeed/i }))
    await user.click(screen.getByRole('button', { name: /Target & network/i }))
    expect(document.querySelector('select[name="interfaceName"] option[value="wg0"]')).not.toBeInTheDocument()
    await user.click(screen.getByRole('switch', { name: /Show virtual paths/i }))
    expect(document.querySelector('select[name="interfaceName"] option[value="wg0"]')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Fixed server/i }))
    await selectSourcePath(user)
    await user.click(screen.getByRole('button', { name: /^Discover$/i }))
    expect(await screen.findByRole('option', { name: /Frankfurt/i })).toBeInTheDocument()
    expect(api.providerServers).toHaveBeenCalledWith('librespeed', expect.objectContaining({ ipFamily: 'auto' }))
  })

  it('offers arbitrary supported IANA zones and previews in the task timezone', async () => {
    mockEditorApi()
    const user = userEvent.setup()
    renderPage(<TaskEditorPage />, { route: '/tasks/new' })
    await screen.findByText('Identity & measurement provider')
    await user.click(screen.getByRole('button', { name: /Schedule & safety/i }))
    const timezone = document.querySelector<HTMLSelectElement>('select[name="timezone"]')
    expect(timezone).not.toBeNull()
    await user.selectOptions(timezone as HTMLSelectElement, 'Asia/Kolkata')
    expect(timezone).toHaveValue('Asia/Kolkata')
    expect((await screen.findAllByText(/\(Asia\/Kolkata\)/)).length).toBeGreaterThan(0)
  })

  it('round-trips unrecognized custom-server and provider option keys', () => {
    const existing = { id: 'task-1', name: 'Libre path', description: '', enabled: false, provider: 'librespeed', cronExpression: '0 * * * *', timezone: 'UTC', randomJitterSeconds: 0, serverSelectionMode: 'custom', serverId: '', serverUrl: 'https://speed.example.test', customServerDefinition: { name: 'Old', futureKey: { enabled: true } }, interfaceName: 'eth0', sourceIp: '192.0.2.10', ipFamily: 'ipv4', routeProfileId: null, timeoutSeconds: 120, providerOptions: { futureOption: 7 }, preventOverlap: true, routeValidation: 'required', createdAt: '', updatedAt: '', lastScheduledAt: null, nextScheduledAt: null } satisfies Task
    const values: TaskFormValues = { name: 'Libre path', description: '', enabled: false, provider: 'librespeed', cronExpression: '0 * * * *', timezone: 'UTC', randomJitterSeconds: 0, serverSelectionMode: 'custom', serverId: '', serverUrl: 'https://speed.example.test', customServerName: 'New', customDownloadPath: 'download', customUploadPath: 'upload', customPingPath: 'ping', customIpPath: 'ip', allowInsecureCustomServer: false, interfaceName: 'eth0', sourceIp: '192.0.2.10', ipFamily: 'ipv4', routeProfileId: '', timeoutSeconds: 120, preventOverlap: true, routeValidation: 'required', skipTlsVerification: true }
    const input = buildTaskInput(values, existing)
    expect(input.customServerDefinition).toMatchObject({ name: 'New', dlURL: 'download', futureKey: { enabled: true } })
    expect(input.providerOptions).toMatchObject({ futureOption: 7, telemetry: false, skipTlsVerification: true })
  })
})

async function selectSourcePath(user: ReturnType<typeof userEvent.setup>) {
  const interfaceSelect = document.querySelector<HTMLSelectElement>('select[name="interfaceName"]')
  const sourceSelect = document.querySelector<HTMLSelectElement>('select[name="sourceIp"]')
  expect(interfaceSelect).not.toBeNull()
  expect(sourceSelect).not.toBeNull()
  await user.selectOptions(interfaceSelect as HTMLSelectElement, 'eth0')
  await waitFor(() => expect(sourceSelect).not.toBeDisabled())
  await user.selectOptions(sourceSelect as HTMLSelectElement, '192.0.2.10')
}
