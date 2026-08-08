import { CheckCircle2, CircleAlert, Info, X } from 'lucide-react'
import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import { cn } from '../../lib/utils'

type ToastTone = 'success' | 'error' | 'info'
interface ToastItem { id: number; title: string; description?: string; tone: ToastTone }
interface ToastApi { toast: (item: Omit<ToastItem, 'id'>) => void }

const ToastContext = createContext<ToastApi | null>(null)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const nextId = useRef(1)
  const remove = useCallback((id: number) => setItems((current) => current.filter((item) => item.id !== id)), [])
  const toast = useCallback((item: Omit<ToastItem, 'id'>) => {
    const id = nextId.current++
    setItems((current) => [...current.slice(-3), { ...item, id }])
    window.setTimeout(() => remove(id), 5000)
  }, [remove])
  const value = useMemo(() => ({ toast }), [toast])
  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="pointer-events-none fixed bottom-5 right-5 z-[100] flex w-[calc(100%-2.5rem)] max-w-sm flex-col gap-2" aria-live="polite">
        {items.map((item) => {
          const Icon = item.tone === 'success' ? CheckCircle2 : item.tone === 'error' ? CircleAlert : Info
          return (
            <div key={item.id} className="pointer-events-auto flex items-start gap-3 rounded-xl border border-border bg-card p-4 text-card-foreground shadow-2xl">
              <Icon className={cn('mt-0.5 h-4 w-4 shrink-0', item.tone === 'success' ? 'text-emerald-500' : item.tone === 'error' ? 'text-rose-500' : 'text-cyan-500')} />
              <div className="min-w-0 flex-1"><p className="text-sm font-semibold">{item.title}</p>{item.description ? <p className="mt-1 text-xs leading-5 text-muted-foreground">{item.description}</p> : null}</div>
              <button type="button" onClick={() => remove(item.id)} className="rounded p-1 text-muted-foreground hover:bg-accent" aria-label="Dismiss notification"><X className="h-3.5 w-3.5" /></button>
            </div>
          )
        })}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastApi {
  const context = useContext(ToastContext)
  if (!context) throw new Error('useToast must be used within ToastProvider')
  return context
}
