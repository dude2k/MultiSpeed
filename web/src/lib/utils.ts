import { clsx, type ClassValue } from 'clsx'
import { format, formatDistanceToNowStrict, isValid, parseISO } from 'date-fns'
import { twMerge } from 'tailwind-merge'
import type { ProviderId, ResultStatus, Settings } from './types'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatBitrate(value: number | null | undefined, compact = false, displayUnits: Settings['displayUnits'] = 'bits'): string {
  if (value == null || !Number.isFinite(value)) return '—'
  const normalized = displayUnits === 'bytes' ? value / 8 : value
  const units = [
    { threshold: 1e9, divisor: 1e9, label: displayUnits === 'bytes' ? 'GB/s' : 'Gbps' },
    { threshold: 1e6, divisor: 1e6, label: displayUnits === 'bytes' ? 'MB/s' : 'Mbps' },
    { threshold: 1e3, divisor: 1e3, label: displayUnits === 'bytes' ? 'kB/s' : 'Kbps' },
  ]
  const unit = units.find((item) => normalized >= item.threshold)
  if (!unit) return `${Math.round(normalized)} ${displayUnits === 'bytes' ? 'B/s' : 'bps'}`
  const amount = normalized / unit.divisor
  return `${amount.toFixed(amount >= 100 || compact ? 0 : 1)} ${unit.label}`
}

export function formatMilliseconds(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  return `${value < 10 ? value.toFixed(1) : value.toFixed(0)} ms`
}

export function formatPercent(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  return `${value.toFixed(value < 10 ? 1 : 0)}%`
}

export function formatBytes(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let amount = value
  let index = 0
  while (amount >= 1000 && index < units.length - 1) {
    amount /= 1000
    index += 1
  }
  return `${amount.toFixed(index === 0 || amount >= 100 ? 0 : 1)} ${units[index] ?? 'B'}`
}

export function formatDateTime(value: string | null | undefined, withSeconds = false, timeZone?: string): string {
  if (!value) return '—'
  const date = parseISO(value)
  if (!isValid(date)) return '—'
  if (!timeZone) return format(date, withSeconds ? 'dd MMM yyyy, HH:mm:ss' : 'dd MMM yyyy, HH:mm')
  return new Intl.DateTimeFormat('en-GB', {
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    ...(withSeconds ? { second: '2-digit' } : {}),
    hourCycle: 'h23',
    timeZone,
  }).format(date)
}

export function formatRelative(value: string | null | undefined): string {
  if (!value) return 'Not scheduled'
  const date = parseISO(value)
  if (!isValid(date)) return 'Unknown'
  return formatDistanceToNowStrict(date, { addSuffix: true })
}

export function providerLabel(provider: ProviderId): string {
  return provider === 'ookla' ? 'Ookla' : provider === 'librespeed' ? 'LibreSpeed' : 'Cloudflare'
}

export const providerColors: Record<ProviderId, string> = {
  ookla: '#67e8f9',
  librespeed: '#a78bfa',
  cloudflare: '#fb923c',
}

export const statusTone: Record<ResultStatus, 'neutral' | 'info' | 'success' | 'warning' | 'danger'> = {
  queued: 'neutral',
  validating: 'info',
  running: 'info',
  succeeded: 'success',
  failed: 'danger',
  skipped: 'warning',
  cancelled: 'neutral',
}

export function isRunning(status: ResultStatus): boolean {
  return status === 'queued' || status === 'validating' || status === 'running'
}

export async function copyText(value: string): Promise<void> {
  await navigator.clipboard.writeText(value)
}

export function downloadFromUrl(url: string, filename: string): void {
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.rel = 'noopener'
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
}

export function safeExternalHttpUrl(value: string | null | undefined): string | null {
  if (!value) return null
  try {
    const parsed = new URL(value)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.href : null
  } catch {
    return null
  }
}

export function reportingDateRange(fromDate: string, toDate: string, timeZone: string): { from: string; to: string } {
  return { from: reportingDateStart(fromDate, timeZone), to: reportingDateEndExclusive(toDate, timeZone) }
}

export function reportingDateStart(dateValue: string, timeZone: string): string { return zonedMidnight(dateValue, timeZone) }
export function reportingDateEndExclusive(dateValue: string, timeZone: string): string { return zonedMidnight(addUtcDays(dateValue, 1), timeZone) }

function zonedMidnight(dateValue: string, timeZone: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dateValue)
  if (!match) throw new Error(`Invalid reporting date: ${dateValue}`)
  const desired = Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
  let candidate = desired
  const formatter = new Intl.DateTimeFormat('en-US', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' })
  for (let attempt = 0; attempt < 4; attempt += 1) {
    const values = Object.fromEntries(formatter.formatToParts(new Date(candidate)).filter((part) => part.type !== 'literal').map((part) => [part.type, Number(part.value)]))
    const represented = Date.UTC(values.year ?? 0, (values.month ?? 1) - 1, values.day ?? 1, values.hour ?? 0, values.minute ?? 0, values.second ?? 0)
    const adjustment = desired - represented
    candidate += adjustment
    if (adjustment === 0) break
  }
  return new Date(candidate).toISOString()
}

function addUtcDays(dateValue: string, days: number): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dateValue)
  if (!match) throw new Error(`Invalid reporting date: ${dateValue}`)
  return new Date(Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3]) + days)).toISOString().slice(0, 10)
}

export function exhaustive(value: never): never {
  throw new Error(`Unexpected value: ${String(value)}`)
}
