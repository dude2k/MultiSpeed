import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { queryKeys } from '../lib/query'
import type { Settings } from '../lib/types'
import { formatBitrate, formatDateTime } from '../lib/utils'

export const fallbackSettings: Settings = {
  displayUnits: 'bits',
  defaultTimezone: 'UTC',
  globalConcurrency: 1,
  allowSeparateWanConcurrency: false,
  retentionMode: 'forever',
  retentionValue: 0,
  defaultChartRange: '30d',
  interfaceRefreshIntervalSeconds: 60,
  defaultTaskTimeoutSeconds: 120,
  databaseMaintenanceSchedule: '30 3 * * *',
  ooklaEulaAccepted: false,
  ooklaEulaAcceptedAt: null,
  ooklaEulaVersion: '',
  ooklaEulaCurrentVersion: '',
  ooklaEulaEffectiveAccepted: false,
  ooklaEulaAcceptanceSource: 'none',
}

export function useAppSettings() {
  const query = useQuery({
    queryKey: queryKeys.settings,
    queryFn: api.settings,
    placeholderData: fallbackSettings,
  })
  return { ...query, settings: query.data ?? fallbackSettings }
}

export function useFormatters() {
  const { settings } = useAppSettings()
  return useMemo(() => ({
    bitrate: (value: number | null | undefined, compact = false) => formatBitrate(value, compact, settings.displayUnits),
    dateTime: (value: string | null | undefined, withSeconds = false) => formatDateTime(value, withSeconds, settings.defaultTimezone),
  }), [settings.defaultTimezone, settings.displayUnits])
}
