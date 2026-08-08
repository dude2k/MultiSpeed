import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router'
import { App } from './App'
import { queryClient } from './lib/query'
import { ToastProvider } from './components/ui/toast'
import './index.css'

const storedTheme = localStorage.getItem('multispeed-theme')
const dark = storedTheme === 'dark' || ((storedTheme === null || storedTheme === 'system') && window.matchMedia('(prefers-color-scheme: dark)').matches)
document.documentElement.classList.toggle('dark', dark)

const root = document.getElementById('root')
if (!root) throw new Error('Application root is missing')

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <ToastProvider><App /></ToastProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
