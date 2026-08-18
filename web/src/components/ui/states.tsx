import { AlertTriangle, Inbox, LoaderCircle, RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
import { useI18n } from '../../i18n'
import { getApiErrorMessage } from '../../lib/api'
import { cn } from '../../lib/utils'
import { Button } from './button'

export function Spinner({ className }: { className?: string }) {
  return <LoaderCircle className={cn('h-4 w-4 animate-spin', className)} aria-hidden="true" />
}

export function LoadingState({ label = 'Loading data…', compact = false }: { label?: string; compact?: boolean }) {
  const { t } = useI18n()
  return <div className={cn('flex items-center justify-center gap-2 text-sm text-muted-foreground', compact ? 'py-6' : 'min-h-52 py-12')}><Spinner /><span>{t(label)}</span></div>
}

export function EmptyState({ icon, title, description, action, compact = false }: { icon?: ReactNode; title: string; description: string; action?: ReactNode; compact?: boolean }) {
  const { t } = useI18n()
  return (
    <div className={cn('flex flex-col items-center justify-center px-5 text-center', compact ? 'py-8' : 'min-h-56 py-12')}>
      <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-xl border border-border bg-muted text-muted-foreground">{icon ?? <Inbox className="h-5 w-5" />}</div>
      <h3 className="text-sm font-semibold text-foreground">{t(title)}</h3>
      <p className="mt-1 max-w-md text-xs leading-5 text-muted-foreground">{t(description)}</p>
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  )
}

export function ErrorState({ error, onRetry, title = 'Unable to load data', compact = false }: { error: unknown; onRetry?: () => void; title?: string; compact?: boolean }) {
  const { t } = useI18n()
  return (
    <div className={cn('flex flex-col items-center justify-center px-5 text-center', compact ? 'py-8' : 'min-h-56 py-12')} role="alert">
      <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-xl border border-rose-500/20 bg-rose-500/10 text-rose-500"><AlertTriangle className="h-5 w-5" /></div>
      <h3 className="text-sm font-semibold text-foreground">{t(title)}</h3>
      <p className="mt-1 max-w-md text-xs leading-5 text-muted-foreground">{getApiErrorMessage(error)}</p>
      {onRetry ? <Button className="mt-4" variant="outline" size="sm" onClick={onRetry}><RefreshCw className="h-3.5 w-3.5" />{t('Try again')}</Button> : null}
    </div>
  )
}

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn('animate-pulse rounded-md bg-muted', className)} />
}
