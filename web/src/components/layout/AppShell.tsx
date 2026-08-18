import * as DialogPrimitive from '@radix-ui/react-dialog'
import {
  Activity,
  BarChart3,
  Columns3,
  Gauge,
  Info,
  ListChecks,
  Menu,
  Moon,
  Network,
  Settings,
  Sun,
  X,
} from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { NavLink, useLocation } from 'react-router'
import { useEventStream, type StreamState } from '../../hooks/useEventStream'
import { useTheme } from '../../hooks/useTheme'
import { type Language, useI18n } from '../../i18n'
import type { ThemeMode } from '../../lib/types'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'

const navigation = [
  { href: '/', label: 'Dashboard', icon: Gauge, subtitle: 'Live overview' },
  { href: '/tasks', label: 'Tasks', icon: ListChecks, subtitle: 'Schedules & targets' },
  { href: '/results', label: 'Results', icon: Activity, subtitle: 'Test history' },
  { href: '/statistics', label: 'Statistics', icon: BarChart3, subtitle: 'Trends & percentiles' },
  { href: '/comparison', label: 'WAN comparison', icon: Columns3, subtitle: 'Path performance' },
  { href: '/network', label: 'Network & routes', icon: Network, subtitle: 'Bindings & validation' },
  { href: '/settings', label: 'Settings', icon: Settings, subtitle: 'Runtime defaults' },
  { href: '/system', label: 'System information', icon: Info, subtitle: 'Health & versions' },
] as const

const pageMeta: Record<string, { title: string; eyebrow: string }> = {
  '/': { title: 'Operations overview', eyebrow: 'Multi-WAN observability' },
  '/tasks': { title: 'Speed-test tasks', eyebrow: 'Independent schedules' },
  '/tasks/new': { title: 'Create task', eyebrow: 'Measurement configuration' },
  '/results': { title: 'Measurement results', eyebrow: 'History & diagnostics' },
  '/statistics': { title: 'Performance statistics', eyebrow: 'Aggregates & trends' },
  '/comparison': { title: 'WAN comparison', eyebrow: 'Side-by-side analysis' },
  '/network': { title: 'Network & routes', eyebrow: 'Read-only path validation' },
  '/settings': { title: 'Application settings', eyebrow: 'Defaults & retention' },
  '/system': { title: 'System information', eyebrow: 'Runtime health' },
}

function Brand() {
  const { t } = useI18n()
  return (
    <NavLink to="/" className="group flex items-center gap-3 rounded-lg focus-visible:ring-2 focus-visible:ring-ring" aria-label={`MultiSpeed ${t('Dashboard')}`}>
      <span className="relative grid h-10 w-10 shrink-0 place-items-center overflow-hidden rounded-xl border border-cyan-400/20 bg-slate-950 shadow-lg shadow-cyan-950/30">
        <span className="flex h-5 items-end gap-[3px]" aria-hidden="true">
          <span className="h-2 w-1 rounded-full bg-cyan-300" />
          <span className="h-4 w-1 rounded-full bg-cyan-400" />
          <span className="h-3 w-1 rounded-full bg-violet-400" />
          <span className="h-5 w-1 rounded-full bg-orange-400" />
        </span>
      </span>
      <span>
        <span className="block text-[17px] font-bold tracking-[-0.03em] text-foreground">MultiSpeed</span>
        <span className="block text-[10px] font-semibold uppercase tracking-[.15em] text-muted-foreground">{t('Path telemetry')}</span>
      </span>
    </NavLink>
  )
}

function Navigation({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useI18n()
  return (
    <nav className="space-y-1" aria-label={t('Navigation')}>
      {navigation.map(({ href, label, icon: Icon, subtitle }) => (
        <NavLink
          key={href}
          to={href}
          end={href === '/'}
          onClick={onNavigate}
          className={({ isActive }) => cn(
            'group flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition focus-visible:ring-2 focus-visible:ring-ring',
            isActive ? 'bg-accent font-semibold text-accent-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
          )}
        >
          {({ isActive }) => <><Icon className={cn('h-4 w-4 shrink-0', isActive ? 'text-primary' : 'text-muted-foreground group-hover:text-foreground')} /><span className="min-w-0"><span className="block leading-4">{t(label)}</span><span className={cn('mt-0.5 block text-[10px] font-normal leading-3', isActive ? 'text-accent-foreground' : 'text-muted-foreground')}>{t(subtitle)}</span></span></>}
        </NavLink>
      ))}
    </nav>
  )
}

function ThemeControl({ mode, setMode }: { mode: ThemeMode; setMode: (mode: ThemeMode) => void }) {
  const { t } = useI18n()
  const options: Array<{ value: ThemeMode; label: string; icon: typeof Sun }> = [
    { value: 'light', label: 'Light theme', icon: Sun },
    { value: 'dark', label: 'Dark theme', icon: Moon },
    { value: 'system', label: 'System theme', icon: Activity },
  ]
  return (
    <div className="flex rounded-lg border border-border bg-muted/50 p-1" aria-label={t('Color theme')}>
      {options.map(({ value, label, icon: Icon }) => <button key={value} type="button" onClick={() => setMode(value)} className={cn('grid h-7 flex-1 place-items-center rounded-md transition focus-visible:ring-2 focus-visible:ring-ring', mode === value ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')} aria-label={t(label)} aria-pressed={mode === value}><Icon className="h-3.5 w-3.5" /></button>)}
    </div>
  )
}

function ConnectionBadge({ state }: { state: StreamState }) {
  const { t } = useI18n()
  const connected = state === 'connected'
  return (
    <div className="flex items-center gap-2 rounded-lg border border-border bg-background px-2.5 py-2 text-[11px] font-medium text-muted-foreground" title={t('Server-sent events connection')}>
      <span className={cn('relative h-2 w-2 rounded-full', connected ? 'bg-emerald-500' : state === 'unavailable' ? 'bg-rose-500' : 'bg-amber-500')}>
        {connected ? <span className="absolute inset-0 animate-ping rounded-full bg-emerald-400 opacity-50" /> : null}
      </span>
      {connected ? t('Live') : state === 'unavailable' ? t('Offline') : t('Reconnecting')}
    </div>
  )
}

function LanguageControl({ language, setLanguage }: { language: Language; setLanguage: (language: Language) => void }) {
  const { t } = useI18n()
  return (
    <div className="flex rounded-lg border border-border bg-muted/50 p-1" aria-label={t('Language')}>
      {(['en', 'de'] as const).map((value) => <button key={value} type="button" onClick={() => setLanguage(value)} className={cn('h-7 flex-1 rounded-md text-[11px] font-semibold transition focus-visible:ring-2 focus-visible:ring-ring', language === value ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')} aria-label={value === 'en' ? t('English') : t('German')} aria-pressed={language === value}>{value.toUpperCase()}</button>)}
    </div>
  )
}

export function AppShell({ children }: { children: ReactNode }) {
  const { pathname } = useLocation()
  const { state } = useEventStream()
  const { theme, setTheme } = useTheme()
  const { language, setLanguage, t } = useI18n()
  const [mobileOpen, setMobileOpen] = useState(false)
  const meta = pathname.startsWith('/tasks/') ? { title: pathname.endsWith('/new') ? 'Create task' : 'Edit task', eyebrow: 'Measurement configuration' } : pathname.startsWith('/results/') ? { title: 'Result diagnostics', eyebrow: 'Measurement detail' } : (pageMeta[pathname] ?? { title: 'MultiSpeed', eyebrow: 'Infrastructure observability' })

  return (
    <div className="min-h-screen bg-background">
      <aside className="fixed inset-y-0 left-0 z-40 hidden w-64 border-r border-border bg-card/95 px-4 py-5 lg:flex lg:flex-col">
        <Brand />
        <div className="mt-7 flex-1 overflow-y-auto scrollbar-thin"><Navigation /></div>
        <div className="mt-4 space-y-3 border-t border-border pt-4">
          <ConnectionBadge state={state} />
          <ThemeControl mode={theme} setMode={setTheme} />
          <LanguageControl language={language} setLanguage={setLanguage} />
          <p className="px-1 text-[10px] leading-4 text-muted-foreground">{t('No authentication. Keep this console on a trusted network.')}</p>
        </div>
      </aside>

      <div className="lg:pl-64">
        <header className="sticky top-0 z-30 border-b border-border bg-background/90 backdrop-blur-xl">
          <div className="flex h-16 items-center gap-3 px-4 sm:px-6 lg:px-8">
            <DialogPrimitive.Root open={mobileOpen} onOpenChange={setMobileOpen}>
              <DialogPrimitive.Trigger asChild><Button size="icon" variant="ghost" className="lg:hidden" aria-label={t('Open navigation')}><Menu className="h-5 w-5" /></Button></DialogPrimitive.Trigger>
              <DialogPrimitive.Portal>
                <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-slate-950/60 backdrop-blur-sm" />
                <DialogPrimitive.Content className="fixed inset-y-0 left-0 z-50 w-[min(88vw,19rem)] border-r border-border bg-card p-5 shadow-2xl outline-none">
                  <DialogPrimitive.Title className="sr-only">{t('Navigation')}</DialogPrimitive.Title>
                  <DialogPrimitive.Description className="sr-only">{t('Navigate between MultiSpeed screens')}</DialogPrimitive.Description>
                  <div className="flex items-start justify-between gap-3"><Brand /><DialogPrimitive.Close asChild><Button size="icon" variant="ghost" aria-label={t('Close navigation')}><X className="h-4 w-4" /></Button></DialogPrimitive.Close></div>
                  <div className="mt-7"><Navigation onNavigate={() => setMobileOpen(false)} /></div>
                  <div className="absolute inset-x-5 bottom-5 space-y-3"><ConnectionBadge state={state} /><ThemeControl mode={theme} setMode={setTheme} /><LanguageControl language={language} setLanguage={setLanguage} /></div>
                </DialogPrimitive.Content>
              </DialogPrimitive.Portal>
            </DialogPrimitive.Root>
            <div className="min-w-0 flex-1">
              <p className="truncate text-[10px] font-bold uppercase tracking-[.13em] text-primary">{t(meta.eyebrow)}</p>
              <h1 className="truncate text-base font-semibold tracking-tight text-foreground">{t(meta.title)}</h1>
            </div>
            <div className="lg:hidden"><ConnectionBadge state={state} /></div>
          </div>
        </header>
        <main className="relative min-h-[calc(100vh-4rem)] overflow-hidden">
          <div className="page-grid pointer-events-none absolute inset-0" aria-hidden="true" />
          <div className="relative mx-auto w-full max-w-[1600px] px-4 py-5 sm:px-6 sm:py-7 lg:px-8 lg:py-8">{children}</div>
        </main>
        <footer className="border-t border-border px-4 py-3 text-center text-[10px] leading-5 text-muted-foreground sm:px-6 lg:px-8">
          {t('Cloudflare® is a trademark of Cloudflare, Inc. Ookla® and Speedtest® are registered trademarks of Ookla, LLC. MultiSpeed is an independent project and is not affiliated with, endorsed by, or sponsored by either company.')}
        </footer>
      </div>
    </div>
  )
}
