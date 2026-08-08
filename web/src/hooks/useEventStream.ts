import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import type { LiveEvent } from '../lib/types'

export type StreamState = 'connecting' | 'connected' | 'reconnecting' | 'unavailable'

const taskEvents = new Set(['task.created', 'task.updated', 'task.enabled', 'task.disabled', 'task.deleted', 'task_created', 'task_updated', 'task_enabled', 'task_disabled', 'task_deleted'])
const resultEvents = new Set(['run.queued', 'route.validation.started', 'route.validation.completed', 'test.started', 'test.completed', 'test.failed', 'test.skipped', 'test.cancelled', 'result.stored', 'run_queued', 'route_validation_started', 'route_validation_completed', 'test_started', 'test_completed', 'test_failed', 'test_skipped', 'new_result_stored'])
export const streamReconnectQueryKeys = [['tasks'], ['results'], ['statistics'], ['interfaces'], ['route-profiles'], ['providers'], ['settings'], ['system']] as const

export function useEventStream(): { state: StreamState; latestEvent: LiveEvent | null } {
  const queryClient = useQueryClient()
  const [state, setState] = useState<StreamState>(() => typeof EventSource === 'undefined' ? 'unavailable' : 'connecting')
  const [latestEvent, setLatestEvent] = useState<LiveEvent | null>(null)

  useEffect(() => {
    if (typeof EventSource === 'undefined') {
      return undefined
    }
    const source = new EventSource(api.eventsUrl, { withCredentials: true })
    source.onopen = () => {
      setState('connected')
      for (const queryKey of streamReconnectQueryKeys) {
        void queryClient.invalidateQueries({ queryKey })
      }
    }
    source.onerror = () => setState(source.readyState === EventSource.CLOSED ? 'unavailable' : 'reconnecting')
    const receive = (event: MessageEvent<string>) => {
      try {
        const parsed = JSON.parse(event.data) as LiveEvent
        if (!parsed.type && event.type !== 'message') parsed.type = event.type
        setLatestEvent(parsed)
        if (taskEvents.has(parsed.type)) void queryClient.invalidateQueries({ queryKey: ['tasks'] })
        if (resultEvents.has(parsed.type)) {
          void queryClient.invalidateQueries({ queryKey: ['results'] })
          void queryClient.invalidateQueries({ queryKey: ['statistics'] })
          void queryClient.invalidateQueries({ queryKey: ['system'] })
        }
        if (parsed.type === 'interface.state.changed' || parsed.type === 'interface_state_changed') void queryClient.invalidateQueries({ queryKey: ['interfaces'] })
        if (parsed.type.includes('route.validation') || parsed.type.includes('route_validation')) void queryClient.invalidateQueries({ queryKey: ['route-profiles'] })
      } catch {
        // Heartbeats may intentionally carry plain text.
      }
    }
    source.onmessage = receive
    const eventNames = [...taskEvents, ...resultEvents, 'interface.state.changed', 'interface_state_changed']
    eventNames.forEach((name) => source.addEventListener(name, receive as EventListener))
    return () => source.close()
  }, [queryClient])

  return { state, latestEvent }
}
