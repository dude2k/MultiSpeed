import type { ReactNode } from 'react'
import { useI18n } from '../../i18n'

export function PageHeader({ title, description, actions }: { title: string; description: string; actions?: ReactNode }) {
  const { t } = useI18n()
  return (
    <div className="mb-6 flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
      <div className="max-w-3xl">
        <h2 className="text-2xl font-bold tracking-[-0.035em] text-foreground sm:text-3xl">{t(title)}</h2>
        <p className="mt-1.5 text-sm leading-6 text-muted-foreground">{t(description)}</p>
      </div>
      {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}

export function SectionHeader({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  const { t } = useI18n()
  return (
    <div className="mb-3 flex items-end justify-between gap-4">
      <div><h3 className="text-sm font-semibold tracking-tight text-foreground">{t(title)}</h3>{description ? <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{t(description)}</p> : null}</div>
      {action}
    </div>
  )
}
