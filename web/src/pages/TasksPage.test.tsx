import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import type { Task } from '../lib/types'
import { renderPage } from '../test/render'
import TasksPage from './TasksPage'

const invalidTask: Task = {
  id: 'task-invalid', name: 'Disconnected WAN', description: 'Former USB uplink', enabled: true, provider: 'librespeed', cronExpression: '0 * * * *', timezone: 'UTC', randomJitterSeconds: 0,
  serverSelectionMode: 'automatic', serverId: '', serverUrl: '', customServerDefinition: {}, interfaceName: 'wwan0', sourceIp: '192.0.2.10', ipFamily: 'ipv4', routeProfileId: null,
  timeoutSeconds: 120, providerOptions: {}, preventOverlap: true, routeValidation: 'required', createdAt: '2026-08-05T00:00:00Z', updatedAt: '2026-08-05T00:00:00Z',
  lastScheduledAt: null, nextScheduledAt: null, networkPathValid: false, networkPathMessage: 'network interface "wwan0" does not exist',
}

describe('task network-path status', () => {
  it('visibly marks a task whose persisted interface or source is unavailable', async () => {
    vi.spyOn(api, 'tasks').mockResolvedValue([invalidTask])

    renderPage(<TasksPage />, { route: '/tasks' })

    expect(await screen.findByText('Invalid path')).toBeVisible()
    expect(screen.getByText('network interface "wwan0" does not exist')).toBeVisible()
    expect(screen.getByRole('link', { name: 'Disconnected WAN' })).toBeVisible()
  })

  it('routes disabled tasks through editor preflight instead of enabling inline', async () => {
    const disabledTask = { ...invalidTask, id: 'task-disabled', name: 'Paused WAN', enabled: false, networkPathValid: true }
    vi.spyOn(api, 'tasks').mockResolvedValue([disabledTask])
    const update = vi.spyOn(api, 'updateTask').mockResolvedValue({ ...disabledTask, enabled: true })
    const user = userEvent.setup()
    renderPage(<TasksPage />, { route: '/tasks' })
    await user.click(await screen.findByRole('switch', { name: 'Enable Paused WAN' }))
    expect(update).not.toHaveBeenCalled()
    expect(await screen.findByText('Preflight required before enabling')).toBeVisible()
  })
})
