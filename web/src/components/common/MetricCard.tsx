import type { LucideIcon } from 'lucide-react'
import { ArrowDownRight, ArrowUpRight, Minus } from 'lucide-react'
import { Card } from '../ui/card'
import { cn } from '../../lib/utils'

export function MetricCard({ label, value, detail, icon: Icon, tone = 'cyan', trend }: { label: string; value: string; detail: string; icon: LucideIcon; tone?: 'cyan' | 'violet' | 'orange' | 'emerald' | 'rose'; trend?: { value: string; direction: 'up' | 'down' | 'flat'; positive?: boolean } }) {
  const toneClass = {
    cyan: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-300',
    violet: 'bg-violet-500/10 text-violet-600 dark:text-violet-300',
    orange: 'bg-orange-500/10 text-orange-600 dark:text-orange-300',
    emerald: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-300',
    rose: 'bg-rose-500/10 text-rose-600 dark:text-rose-300',
  }[tone]
  const TrendIcon = trend?.direction === 'up' ? ArrowUpRight : trend?.direction === 'down' ? ArrowDownRight : Minus
  return (
    <Card className="relative overflow-hidden p-4 sm:p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0"><p className="text-[11px] font-semibold uppercase tracking-[.1em] text-muted-foreground">{label}</p><p className="metric-number mt-2 truncate text-2xl font-bold text-foreground">{value}</p></div>
        <span className={cn('grid h-9 w-9 shrink-0 place-items-center rounded-lg', toneClass)}><Icon className="h-4 w-4" /></span>
      </div>
      <div className="mt-3 flex min-h-5 items-center gap-2 text-[11px] text-muted-foreground">
        {trend ? <span className={cn('inline-flex items-center gap-0.5 font-semibold', trend.positive === true ? 'text-emerald-600 dark:text-emerald-400' : trend.positive === false ? 'text-rose-600 dark:text-rose-400' : 'text-muted-foreground')}><TrendIcon className="h-3 w-3" />{trend.value}</span> : null}
        <span className="truncate">{detail}</span>
      </div>
    </Card>
  )
}
