import * as LabelPrimitive from '@radix-ui/react-label'
import * as SwitchPrimitive from '@radix-ui/react-switch'
import { ChevronDown } from 'lucide-react'
import { cloneElement, forwardRef, isValidElement, useId, type InputHTMLAttributes, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(function Input({ className, type, ...props }, ref) {
  return (
    <input
      ref={ref}
      type={type}
      className={cn(
        'flex h-10 w-full rounded-lg border border-input bg-background px-3 py-2 text-sm text-foreground shadow-sm outline-none transition placeholder:text-muted-foreground/70 focus:border-primary/70 focus:ring-2 focus:ring-primary/15 disabled:cursor-not-allowed disabled:opacity-60',
        className,
      )}
      {...props}
    />
  )
})

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(function Textarea({ className, ...props }, ref) {
  return (
    <textarea
      ref={ref}
      className={cn(
        'flex min-h-24 w-full resize-y rounded-lg border border-input bg-background px-3 py-2 text-sm text-foreground shadow-sm outline-none transition placeholder:text-muted-foreground/70 focus:border-primary/70 focus:ring-2 focus:ring-primary/15 disabled:cursor-not-allowed disabled:opacity-60',
        className,
      )}
      {...props}
    />
  )
})

export const NativeSelect = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(function NativeSelect({ className, children, ...props }, ref) {
  return (
    <div className="relative">
      <select
        ref={ref}
        className={cn(
          'flex h-10 w-full appearance-none rounded-lg border border-input bg-background py-2 pl-3 pr-9 text-sm text-foreground shadow-sm outline-none transition focus:border-primary/70 focus:ring-2 focus:ring-primary/15 disabled:cursor-not-allowed disabled:opacity-60',
          className,
        )}
        {...props}
      >
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
    </div>
  )
})

export function Label({ className, ...props }: React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root>) {
  return <LabelPrimitive.Root className={cn('text-xs font-semibold leading-5 text-foreground', className)} {...props} />
}

export function Field({ label, hint, error, required, children, className }: { label: string; hint?: string | undefined; error?: string | undefined; required?: boolean | undefined; children: React.ReactNode; className?: string | undefined }) {
  const generatedId = useId()
  const controlId = isValidElement<AccessibleControlProps>(children) && children.props.id ? children.props.id : generatedId
  const hintId = hint ? `${controlId}-hint` : undefined
  const errorId = error ? `${controlId}-error` : undefined
  const describedBy = [hintId, errorId].filter(Boolean).join(' ') || undefined
  const accessibleProps: AccessibleControlProps = { id: controlId }
  if (describedBy) accessibleProps['aria-describedby'] = describedBy
  if (error) accessibleProps['aria-invalid'] = true
  if (required) accessibleProps['aria-required'] = true
  const control = isValidElement<AccessibleControlProps>(children)
    ? cloneElement(children, accessibleProps)
    : children
  return (
    <div className={cn('space-y-1.5', className)}>
      <div className="flex items-baseline justify-between gap-2">
        <Label htmlFor={controlId}>
          {label}
          {required ? <span className="ml-1 text-rose-500" aria-hidden="true">*</span> : null}
        </Label>
        {hint ? <span id={hintId} className="text-[11px] text-muted-foreground">{hint}</span> : null}
      </div>
      {control}
      {error ? <p id={errorId} className="text-xs font-medium text-rose-600 dark:text-rose-400" role="alert">{error}</p> : null}
    </div>
  )
}

interface AccessibleControlProps {
  id?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
  'aria-required'?: boolean
}

export function Switch({ checked, onCheckedChange, disabled, id, 'aria-label': ariaLabel }: { checked: boolean; onCheckedChange: (checked: boolean) => void; disabled?: boolean; id?: string; 'aria-label'?: string }) {
  return (
    <SwitchPrimitive.Root
      id={id}
      checked={checked}
      onCheckedChange={onCheckedChange}
      disabled={disabled}
      aria-label={ariaLabel}
      className="relative h-6 w-11 shrink-0 cursor-pointer rounded-full border border-transparent bg-muted-foreground/25 outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring data-[state=checked]:bg-primary disabled:cursor-not-allowed disabled:opacity-50"
    >
      <SwitchPrimitive.Thumb className="block h-5 w-5 translate-x-0 rounded-full bg-white shadow transition-transform data-[state=checked]:translate-x-5" />
    </SwitchPrimitive.Root>
  )
}

export function CheckField({ checked, onChange, label, description, disabled = false }: { checked: boolean; onChange: (checked: boolean) => void; label: string; description?: string; disabled?: boolean }) {
  return (
    <label className={cn('flex cursor-pointer items-start justify-between gap-4 rounded-lg border border-border bg-background p-3', disabled && 'cursor-not-allowed opacity-60')}>
      <span>
        <span className="block text-sm font-medium text-foreground">{label}</span>
        {description ? <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">{description}</span> : null}
      </span>
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} aria-label={label} />
    </label>
  )
}
