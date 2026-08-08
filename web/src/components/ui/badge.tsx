import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

export type BadgeTone = 'neutral' | 'info' | 'success' | 'warning' | 'danger' | 'violet'

const tones: Record<BadgeTone, string> = {
  neutral: 'border-border bg-muted text-muted-foreground',
  info: 'border-cyan-500/25 bg-cyan-500/10 text-cyan-700 dark:text-cyan-300',
  success: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
  warning: 'border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300',
  danger: 'border-rose-500/25 bg-rose-500/10 text-rose-700 dark:text-rose-300',
  violet: 'border-violet-500/25 bg-violet-500/10 text-violet-700 dark:text-violet-300',
}

export function Badge({ className, tone = 'neutral', ...props }: HTMLAttributes<HTMLSpanElement> & { tone?: BadgeTone }) {
  return <span className={cn('inline-flex min-h-5 items-center rounded-full border px-2 py-0.5 text-[11px] font-semibold leading-4', tones[tone], className)} {...props} />
}

export function StatusDot({ active, className }: { active: boolean; className?: string }) {
  return <span className={cn('inline-block h-1.5 w-1.5 rounded-full', active ? 'bg-emerald-500' : 'bg-muted-foreground/50', className)} aria-hidden="true" />
}
