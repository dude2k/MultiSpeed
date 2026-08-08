import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import { fallbackSettings } from '../hooks/useAppSettings'
import { renderPage } from '../test/render'
import ResultsPage from './ResultsPage'

describe('result filters and empty states', () => {
  it('exposes task, provider, WAN, status, and date filters', async () => {
    vi.spyOn(api, 'results').mockResolvedValue({ items: [], page: 1, pageSize: 25, totalItems: 0, totalPages: 1 })
    vi.spyOn(api, 'tasks').mockResolvedValue([])
    vi.spyOn(api, 'interfaces').mockResolvedValue([])
    vi.spyOn(api, 'settings').mockResolvedValue(fallbackSettings)
    renderPage(<ResultsPage />, { route: '/results' })
    expect(await screen.findByText('No measurements yet')).toBeInTheDocument()
    expect(screen.getByLabelText('Filter results by task')).toBeInTheDocument()
    expect(screen.getByLabelText('Filter results by provider')).toBeInTheDocument()
    expect(screen.getByLabelText('Filter results by WAN')).toBeInTheDocument()
    expect(screen.getByLabelText('Filter results by status')).toBeInTheDocument()
    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Filter results by status'), 'failed')
    expect(await screen.findByText('No results match these filters')).toBeInTheDocument()
  })
})
