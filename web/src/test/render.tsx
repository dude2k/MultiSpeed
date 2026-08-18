import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, type RenderOptions } from '@testing-library/react'
import type { ReactElement } from 'react'
import { MemoryRouter } from 'react-router'
import { ToastProvider } from '../components/ui/toast'
import { I18nProvider } from '../i18n'

export function renderPage(ui: ReactElement, options: RenderOptions & { route?: string } = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } } })
  const { route = '/', ...renderOptions } = options
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>
        <I18nProvider><ToastProvider>{ui}</ToastProvider></I18nProvider>
      </MemoryRouter>
    </QueryClientProvider>,
    renderOptions,
  )
}
