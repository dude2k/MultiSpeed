import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ApiError, api } from '../lib/api'
import { fallbackSettings } from '../hooks/useAppSettings'
import { renderPage } from '../test/render'
import StatisticsPage from './StatisticsPage'

describe('statistics query errors', () => {
  it('keeps query controls usable and hides report output after an actionable 413', async () => {
    vi.spyOn(api, 'tasks').mockResolvedValue([])
    vi.spyOn(api, 'interfaces').mockResolvedValue([])
    vi.spyOn(api, 'routes').mockResolvedValue([])
    vi.spyOn(api, 'results').mockResolvedValue({ items: [], page: 1, pageSize: 200, totalItems: 0, totalPages: 0 })
    vi.spyOn(api, 'settings').mockResolvedValue(fallbackSettings)
    const statistics = vi.spyOn(api, 'statistics').mockRejectedValue(new ApiError(
      'The query would produce more than 5,000 output points or 1,000 groups; use a coarser granularity, shorter range, or narrower filters.',
      413,
      'STATISTICS_OUTPUT_LIMIT_EXCEEDED',
    ))
    const user = userEvent.setup()

    renderPage(<StatisticsPage />)

    expect(await screen.findByRole('alert')).toHaveTextContent('use a coarser granularity, shorter range, or narrower filters')
    expect(screen.getByText('Patterns beyond a single test.')).toBeVisible()

    const rawAggregation = screen.getByRole('button', { name: /raw/i })
    await user.click(rawAggregation)
    expect(rawAggregation).toHaveAttribute('aria-pressed', 'true')

    const dateInputs = document.querySelectorAll<HTMLInputElement>('input[type="date"]')
    expect(dateInputs).toHaveLength(2)
    expect(dateInputs[0]).toBeEnabled()
    expect(dateInputs[1]).toBeEnabled()

    await user.click(screen.getByText('Providers'))
    const cloudflareFilter = screen.getByRole('checkbox', { name: 'Cloudflare' })
    await user.click(cloudflareFilter)
    expect(cloudflareFilter).toBeChecked()

    expect(screen.queryByText('Standard deviation')).not.toBeInTheDocument()
    expect(screen.queryByText('Download throughput trend')).not.toBeInTheDocument()
    expect(screen.queryByText('Accessible statistical table')).not.toBeInTheDocument()

    const callsBeforeRetry = statistics.mock.calls.length
    await user.click(screen.getByRole('button', { name: /try again/i }))
    await waitFor(() => expect(statistics.mock.calls.length).toBeGreaterThan(callsBeforeRetry))
  })
})
