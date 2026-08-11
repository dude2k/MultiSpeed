import { expect, test, type Page } from '@playwright/test'

interface NetworkInterface {
  name: string
  operational: boolean
  loopback: boolean
  virtual: boolean
  addresses: Array<{ address: string; family: 'ipv4' | 'ipv6'; linkLocal: boolean }>
}

interface TaskRecord { id: string; name: string }
interface RouteRecord { id: string; name: string }
interface ResultRecord {
  id: string
  taskId: string
  status: string
  downloadBitsPerSecond: number | null
  uploadBitsPerSecond: number | null
  latencyMilliseconds: number | null
  detectedPublicIp: string
  serverName: string
  rawProviderResponse: string
  providerVersion: string
  applicationVersion: string
  tlsVerificationDisabled: boolean
}

interface PageResponse<T> { items: T[] }
interface ConfigurationDocument {
  format: string
  version: number
  settings: { defaultChartRange: string }
  tasks: unknown[]
  routeProfiles: unknown[]
  [key: string]: unknown
}

test('complete operator workflow through the real backend and fake provider executables', async ({ page }) => {
  test.setTimeout(120_000)

  const healthResponse = await page.request.get('/api/v1/healthz')
  expect(healthResponse.ok()).toBe(true)
  expect(await healthResponse.json()).toMatchObject({ status: 'ok' })

  const systemResponse = await page.request.get('/api/v1/system')
  expect(systemResponse.ok()).toBe(true)
  const system = await systemResponse.json() as { version: string }
  expect(system.version).toBe('e2e-test')

  const providersResponse = await page.request.get('/api/v1/providers')
  expect(providersResponse.ok()).toBe(true)
  const providers = await providersResponse.json() as Array<{ id: string; available: boolean; version: string }>
  expect(providers.find((provider) => provider.id === 'ookla')).toMatchObject({ available: false })
  expect(providers.find((provider) => provider.id === 'librespeed')).toMatchObject({ available: true, version: 'librespeed-cli v1.0.13+multispeed.dns2.xnet055 deterministic-e2e' })

  await page.goto('/settings')
  await expect(page.getByText('Ookla provider terms & authorization')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Record acknowledgement' })).toBeDisabled()
  await page.getByRole('switch', { name: 'I agree to the current EULA and Terms of Use and reviewed the Privacy Policy' }).click()
  await page.getByRole('button', { name: 'Record acknowledgement' }).click()
  await expect(page.getByText('Acknowledgement recorded', { exact: true })).toBeVisible()

  const acceptedProvidersResponse = await page.request.get('/api/v1/providers')
  expect(acceptedProvidersResponse.ok()).toBe(true)
  const acceptedProviders = await acceptedProvidersResponse.json() as Array<{ id: string; available: boolean; version: string }>
  expect(acceptedProviders.find((provider) => provider.id === 'ookla')).toMatchObject({ available: true, version: 'Speedtest by Ookla 1.2.0 deterministic-e2e' })

  const paths = await discoverNetworkPaths(page)
  const path = paths[0]
  await page.goto('/network')
  await expect(page.getByRole('heading', { name: 'Observe routes. Never rewrite them.' })).toBeVisible()
  await expect(page.getByText(path.interfaceName).first()).toBeVisible()
  await page.getByRole('button', { name: /new route profile/i }).first().click()
  await page.locator('[name="name"]').fill('E2E route fixture')
  await page.locator('[name="validationTarget"]').fill('192.0.2.254')
  await page.locator('[name="interfaceName"]').selectOption(path.interfaceName)
  await page.locator('[name="sourceIp"]').selectOption(path.sourceIp)
  await page.getByRole('button', { name: 'Create profile' }).click()
  await expect(page.getByText('E2E route fixture').first()).toBeVisible()
  await expect(page.getByText('Not validated')).toBeVisible()

  const routesResponse = await page.request.get('/api/v1/route-profiles')
  expect(routesResponse.ok()).toBe(true)
  const route = (await routesResponse.json() as RouteRecord[]).find((item) => item.name === 'E2E route fixture')
  expect(route).toBeDefined()
  if (!route) throw new Error('The route profile was not persisted by the real backend.')

  await page.getByRole('button', { name: 'Validate now' }).click()
  await expect(page.getByText('Valid', { exact: true })).toBeVisible()

  const cableRoute = await createRouteFixture(page, 'E2E cable route', paths[1])
  const lteRoute = await createRouteFixture(page, 'E2E LTE route', paths[2])

  await createTask(page, { name: 'Ookla fixture', provider: 'Ookla Speedtest', cron: '0 */6 * * *', routeProfileId: route.id, ...path, fixedServer: true })
  await createTask(page, { name: 'LibreSpeed fixture', provider: 'LibreSpeed', cron: '15 */4 * * *', routeProfileId: cableRoute.id, ...paths[1] })
  await createTask(page, { name: 'Cloudflare metadata only', provider: 'Cloudflare', cron: '30 */2 * * *', routeProfileId: lteRoute.id, ...paths[2] })

  await page.goto('/tasks')
  await expect(page.getByRole('link', { name: 'Ookla fixture', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'LibreSpeed fixture', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Cloudflare metadata only', exact: true })).toBeVisible()

  const ooklaResult = await runTaskAndWait(page, 'Ookla fixture')
  expect(ooklaResult).toMatchObject({
    status: 'succeeded', downloadBitsPerSecond: 620_000_000, uploadBitsPerSecond: 118_000_000, latencyMilliseconds: 14.2,
    detectedPublicIp: '203.0.113.10', serverName: 'Frankfurt Fixture', applicationVersion: 'e2e-test', tlsVerificationDisabled: false,
  })
  expect(ooklaResult.providerVersion).toContain('deterministic-e2e')
  expect(ooklaResult.rawProviderResponse).toContain('ookla.fixture.invalid')

  const libreSpeedResult = await runTaskAndWait(page, 'LibreSpeed fixture')
  expect(libreSpeedResult).toMatchObject({
    status: 'succeeded', downloadBitsPerSecond: 540_000_000, uploadBitsPerSecond: 95_000_000, latencyMilliseconds: 18.5,
    detectedPublicIp: '198.51.100.20', serverName: 'Berlin Fixture', applicationVersion: 'e2e-test', tlsVerificationDisabled: false,
  })
  expect(libreSpeedResult.providerVersion).toContain('deterministic-e2e')
  expect(libreSpeedResult.rawProviderResponse).toContain('librespeed.fixture.invalid')

  await page.goto('/results')
  await expect(page.getByText('620 Mbps').first()).toBeVisible()
  await expect(page.getByText('540 Mbps').first()).toBeVisible()
  await page.getByRole('link', { name: 'Ookla fixture' }).first().click()
  await expect(page.getByText('Sanitized provider response')).toBeVisible()
  await expect(page.getByText('203.0.113.10').first()).toBeVisible()

  await page.getByRole('link', { name: 'Statistics' }).first().click()
  await expect(page.getByText('Accessible statistical table')).toBeVisible()
  await expect(page.getByText('580 Mbps').first()).toBeVisible()

  await page.getByRole('link', { name: 'WAN comparison' }).first().click()
  await expect(page.getByText(path.interfaceName).first()).toBeVisible()
  await expect(page.getByText(paths[1].interfaceName).first()).toBeVisible()
  await expect(page.getByText('Comparison matrix')).toBeVisible()
  await expect(page.getByText('1 attempt').first()).toBeVisible()

  const backupResponse = await page.request.post('/api/v1/backup')
  expect(backupResponse.ok()).toBe(true)
  expect(backupResponse.headers()['content-type']).toContain('application/vnd.sqlite3')
  expect((await backupResponse.body()).byteLength).toBeGreaterThan(0)

  const configurationResponse = await page.request.get('/api/v1/config/export')
  expect(configurationResponse.ok()).toBe(true)
  const configuration = await configurationResponse.json() as ConfigurationDocument
  expect(configuration).toMatchObject({ format: 'multispeed-config', version: 1 })
  expect(configuration.tasks).toHaveLength(3)
  expect(configuration.routeProfiles).toHaveLength(3)
  expect(JSON.stringify(configuration)).not.toContain('ooklaEula')
  configuration.settings.defaultChartRange = '7d'
  const importResponse = await page.request.post('/api/v1/config/import', { data: configuration })
  expect(importResponse.ok()).toBe(true)
  expect(await importResponse.json()).toMatchObject({ taskCount: 3, routeProfileCount: 3, settingsUpdated: true })
  const postImportSettings = await (await page.request.get('/api/v1/settings')).json() as { defaultChartRange: string; ooklaEulaAccepted: boolean }
  expect(postImportSettings).toMatchObject({ defaultChartRange: '7d', ooklaEulaAccepted: true })

  await page.getByRole('link', { name: 'Tasks' }).first().click()
  await page.getByRole('switch', { name: 'Disable Ookla fixture' }).click()
  await expect(page.getByRole('switch', { name: 'Enable Ookla fixture' })).toBeVisible()
  await openTaskMenu(page, 'LibreSpeed fixture')
  await page.getByRole('menuitem', { name: 'Duplicate' }).click()
  await expect(page).toHaveURL(/\/tasks\/[a-f0-9-]+\/edit/)
  await page.getByRole('link', { name: /back to tasks/i }).click()
  await expect(page.getByRole('link', { name: 'LibreSpeed fixture copy', exact: true })).toBeVisible()
  await openTaskMenu(page, 'Cloudflare metadata only')
  await page.getByRole('menuitem', { name: 'Delete' }).click()
  await page.getByRole('button', { name: 'Delete task' }).click()
  await expect(page.getByRole('link', { name: 'Cloudflare metadata only', exact: true })).toHaveCount(0)
})

async function discoverNetworkPaths(page: Page): Promise<Array<{ interfaceName: string; sourceIp: string }>> {
  const response = await page.request.get('/api/v1/interfaces?includeDown=true&includeVirtual=true')
  expect(response.ok()).toBe(true)
  const payload = await response.json() as { items: NetworkInterface[] }
  const paths: Array<{ interfaceName: string; sourceIp: string }> = []
  for (const item of payload.items) {
	if (!item.operational || item.loopback || item.virtual) continue
	const address = item.addresses.find((candidate) => candidate.family === 'ipv4' && !candidate.linkLocal)
	if (address) paths.push({ interfaceName: item.name, sourceIp: address.address })
  }
	if (paths.length < 3) throw new Error('The deterministic backend did not expose three simulated WAN paths.')
	return paths.slice(0, 3)
}

async function createRouteFixture(page: Page, name: string, path: { interfaceName: string; sourceIp: string }): Promise<RouteRecord> {
  const response = await page.request.post('/api/v1/route-profiles', { data: {
    name, description: 'Deterministic simulated WAN route', interfaceName: path.interfaceName, sourceIp: path.sourceIp,
    expectedGateway: '', expectedRoutingTable: '', validationTarget: '192.0.2.254', notes: '',
  } })
  expect(response.ok()).toBe(true)
  return await response.json() as RouteRecord
}

async function createTask(page: Page, options: { name: string; provider: string; interfaceName: string; sourceIp: string; cron: string; routeProfileId: string; fixedServer?: boolean }): Promise<void> {
  await page.goto('/tasks/new')
  await page.locator('[name="name"]').fill(options.name)
  await page.getByRole('button', { name: new RegExp(options.provider, 'i') }).click()
  await page.getByRole('button', { name: 'Continue' }).click()
  if (options.provider === 'Cloudflare') await expect(page.getByText('Automatic edge selection')).toBeVisible()
  await page.locator('[name="interfaceName"]').selectOption(options.interfaceName)
  await page.locator('[name="sourceIp"]').selectOption(options.sourceIp)
  await page.locator('[name="routeProfileId"]').selectOption(options.routeProfileId)
  if (options.fixedServer) {
    await page.getByRole('button', { name: /fixed server/i }).click()
    await page.getByRole('button', { name: 'Discover', exact: true }).click()
    const server = page.getByRole('option', { name: /Frankfurt Fixture/ })
    await expect(server).toBeVisible()
    await server.click()
    await expect(page.getByText('Persisted server ID:')).toBeVisible()
  }
  await page.getByRole('button', { name: 'Continue' }).click()
  await page.locator('[name="cronExpression"]').fill(options.cron)
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByText('Review this independent path')).toBeVisible()
  await page.getByRole('button', { name: 'Validate current configuration' }).click()
  await expect(page.getByText('Current configuration passed preflight')).toBeVisible()
  await page.locator('form').evaluate((form) => (form as unknown as { requestSubmit: () => void }).requestSubmit())
  await expect(page).toHaveURL(/\/tasks$/)
  await expect(page.getByRole('link', { name: options.name, exact: true })).toBeVisible()
}

async function openTaskMenu(page: Page, taskName: string): Promise<void> {
  const row = page.getByRole('row').filter({ hasText: taskName })
  await row.getByRole('button', { name: `Actions for ${taskName}` }).click()
}

async function runTaskAndWait(page: Page, taskName: string): Promise<ResultRecord> {
  const tasksResponse = await page.request.get('/api/v1/tasks')
  expect(tasksResponse.ok()).toBe(true)
  const task = (await tasksResponse.json() as TaskRecord[]).find((item) => item.name === taskName)
  expect(task).toBeDefined()
  if (!task) throw new Error(`Task ${taskName} was not persisted.`)

  await openTaskMenu(page, taskName)
  await page.getByRole('menuitem', { name: 'Run now' }).click()
  await expect(page.getByText('Test queued').last()).toBeVisible()
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/results?page=1&pageSize=10&taskId=${encodeURIComponent(task.id)}`)
    if (!response.ok()) return `http-${response.status()}`
    const payload = await response.json() as PageResponse<Omit<ResultRecord, 'rawProviderResponse'>>
    return payload.items[0]?.status ?? 'missing'
  }, { message: `${taskName} should finish through the real execution pipeline`, timeout: 30_000 }).toBe('succeeded')

  const response = await page.request.get(`/api/v1/results?page=1&pageSize=10&taskId=${encodeURIComponent(task.id)}`)
  const payload = await response.json() as PageResponse<Omit<ResultRecord, 'rawProviderResponse'>>
  const result = payload.items[0]
  if (!result) throw new Error(`The real backend did not persist a result for ${taskName}.`)
  expect(result).not.toHaveProperty('rawProviderResponse')
  const detailResponse = await page.request.get(`/api/v1/results/${encodeURIComponent(result.id)}`)
  expect(detailResponse.ok()).toBe(true)
  return await detailResponse.json() as ResultRecord
}
