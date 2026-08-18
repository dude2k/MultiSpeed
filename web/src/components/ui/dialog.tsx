import * as AlertDialogPrimitive from '@radix-ui/react-alert-dialog'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import type { ReactNode } from 'react'
import { useI18n } from '../../i18n'
import { cn } from '../../lib/utils'
import { Button } from './button'

export const Dialog = DialogPrimitive.Root
export const DialogTrigger = DialogPrimitive.Trigger

export function DialogContent({ children, className, title, description }: { children: ReactNode; className?: string; title: string; description?: string }) {
  const { t } = useI18n()
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-slate-950/65 backdrop-blur-[2px] data-[state=closed]:animate-out" />
      <DialogPrimitive.Content className={cn('fixed left-1/2 top-1/2 z-50 max-h-[92vh] w-[calc(100%-2rem)] max-w-xl -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-xl border border-border bg-card p-6 text-card-foreground shadow-2xl outline-none', className)}>
        <div className="pr-8">
          <DialogPrimitive.Title className="text-lg font-semibold tracking-tight">{t(title)}</DialogPrimitive.Title>
          {description ? <DialogPrimitive.Description className="mt-1 text-sm leading-5 text-muted-foreground">{t(description)}</DialogPrimitive.Description> : null}
        </div>
        <DialogPrimitive.Close className="absolute right-4 top-4 rounded-md p-1.5 text-muted-foreground transition hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={t('Close')}>
          <X className="h-4 w-4" />
        </DialogPrimitive.Close>
        <div className="mt-5">{children}</div>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  )
}

export function ConfirmDialog({ open, onOpenChange, title, description, confirmLabel, onConfirm, destructive = false, busy = false }: { open: boolean; onOpenChange: (open: boolean) => void; title: string; description: ReactNode; confirmLabel: string; onConfirm: () => void; destructive?: boolean; busy?: boolean }) {
  const { t } = useI18n()
  return (
    <AlertDialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <AlertDialogPrimitive.Portal>
        <AlertDialogPrimitive.Overlay className="fixed inset-0 z-50 bg-slate-950/65 backdrop-blur-[2px]" />
        <AlertDialogPrimitive.Content className="fixed left-1/2 top-1/2 z-50 w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl border border-border bg-card p-6 shadow-2xl outline-none">
          <AlertDialogPrimitive.Title className="text-lg font-semibold text-foreground">{t(title)}</AlertDialogPrimitive.Title>
          <AlertDialogPrimitive.Description asChild>
            <div className="mt-2 text-sm leading-6 text-muted-foreground">{description}</div>
          </AlertDialogPrimitive.Description>
          <div className="mt-6 flex justify-end gap-2">
            <AlertDialogPrimitive.Cancel asChild><Button variant="outline" disabled={busy}>{t('Cancel')}</Button></AlertDialogPrimitive.Cancel>
            <AlertDialogPrimitive.Action asChild><Button variant={destructive ? 'danger' : 'primary'} disabled={busy} onClick={onConfirm}>{busy ? t('Working…') : t(confirmLabel)}</Button></AlertDialogPrimitive.Action>
          </div>
        </AlertDialogPrimitive.Content>
      </AlertDialogPrimitive.Portal>
    </AlertDialogPrimitive.Root>
  )
}
