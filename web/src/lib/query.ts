import { QueryClient } from '@tanstack/react-query'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: (failureCount, error) => {
        if (error instanceof Error && 'status' in error && typeof error.status === 'number' && error.status < 500) return false
        return failureCount < 2
      },
      refetchOnWindowFocus: false,
    },
    mutations: { retry: false },
  },
})

export const queryKeys = {
  tasks: ['tasks'] as const,
  task: (id: string) => ['tasks', id] as const,
  results: (filters: object = {}) => ['results', filters] as const,
  dashboardSummary: ['results', 'dashboard-summary'] as const,
  result: (id: string) => ['results', id] as const,
  statistics: (filters: object = {}) => ['statistics', filters] as const,
  interfaces: ['interfaces'] as const,
  routes: ['route-profiles'] as const,
  providers: ['providers'] as const,
  settings: ['settings'] as const,
  system: ['system'] as const,
}
