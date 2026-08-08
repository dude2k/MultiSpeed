import { useEffect, useState } from 'react'
import type { ThemeMode } from '../lib/types'

const STORAGE_KEY = 'multispeed-theme'

function systemIsDark(): boolean {
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

function applyTheme(mode: ThemeMode): void {
  const dark = mode === 'dark' || (mode === 'system' && systemIsDark())
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.dataset.theme = mode
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', dark ? '#08111f' : '#f7f9fc')
}

export function useTheme() {
  const [theme, setThemeState] = useState<ThemeMode>(() => {
    const stored = localStorage.getItem(STORAGE_KEY)
    return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system'
  })

  useEffect(() => {
    applyTheme(theme)
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const listener = () => theme === 'system' && applyTheme(theme)
    query.addEventListener('change', listener)
    return () => query.removeEventListener('change', listener)
  }, [theme])

  const setTheme = (value: ThemeMode) => {
    localStorage.setItem(STORAGE_KEY, value)
    setThemeState(value)
  }
  return { theme, setTheme }
}
