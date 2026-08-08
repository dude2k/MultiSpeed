import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { streamReconnectQueryKeys, useEventStream } from './useEventStream'

class EventSourceStub {
  static readonly CLOSED = 2
  static instance: EventSourceStub | null = null
  readonly readyState = 1
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent<string>) => void) | null = null

  constructor() { EventSourceStub.instance = this }
  addEventListener(): void {}
  close(): void {}
}

describe('event stream reconnects', () => {
  it('invalidates every REST-backed cache after each connection is established', async () => {
    EventSourceStub.instance = null
    vi.stubGlobal('EventSource', EventSourceStub)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
    const hook = renderHook(() => useEventStream(), { wrapper })
    await waitFor(() => expect(EventSourceStub.instance).not.toBeNull())
    act(() => EventSourceStub.instance?.onopen?.(new Event('open')))
    await waitFor(() => expect(hook.result.current.state).toBe('connected'))
    expect(invalidate.mock.calls.slice(-streamReconnectQueryKeys.length).map((call) => call[0]?.queryKey)).toEqual(streamReconnectQueryKeys)
    act(() => EventSourceStub.instance?.onopen?.(new Event('open')))
    expect(invalidate).toHaveBeenCalledTimes(streamReconnectQueryKeys.length * 2)
  })
})
