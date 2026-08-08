import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { MoreHorizontal } from 'lucide-react'
import type { ComponentPropsWithoutRef, ReactNode } from 'react'
import { cn } from '../../lib/utils'
import { Button } from './button'

export function ActionMenu({ label = 'Open actions', children }: { label?: string; children: ReactNode }) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <Button size="icon" variant="ghost" aria-label={label}><MoreHorizontal className="h-4 w-4" /></Button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="end" sideOffset={6} className="z-50 min-w-44 rounded-lg border border-border bg-card p-1 text-card-foreground shadow-xl">
          {children}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}

export function ActionMenuItem({ className, danger, ...props }: ComponentPropsWithoutRef<typeof DropdownMenu.Item> & { danger?: boolean }) {
  return <DropdownMenu.Item className={cn('flex cursor-pointer select-none items-center gap-2 rounded-md px-2.5 py-2 text-xs font-medium outline-none transition focus:bg-accent', danger && 'text-rose-600 focus:bg-rose-500/10 dark:text-rose-400', className)} {...props} />
}

export const ActionMenuSeparator = () => <DropdownMenu.Separator className="my-1 h-px bg-border" />
