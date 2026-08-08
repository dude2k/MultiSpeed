import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

const apiProxyTarget = process.env.MULTISPEED_API_PROXY_TARGET ?? 'http://127.0.0.1:8787'

export default defineConfig({
  plugins: [react()],
  build: {
    target: 'es2022',
    sourcemap: false,
    chunkSizeWarningLimit: 1200,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('/echarts/')) return 'charts'
          if (id.includes('/react/') || id.includes('/react-dom/') || id.includes('/react-router/')) return 'react'
          if (id.includes('/@tanstack/react-query/') || id.includes('/@tanstack/react-table/')) return 'query'
          return undefined
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: apiProxyTarget,
        changeOrigin: true,
        configure(proxy) {
          proxy.on('proxyReq', (proxyRequest, request) => {
            const origin = request.headers.origin
            if (!origin || !request.headers.host) return
            try {
              if (new URL(origin).host === request.headers.host) proxyRequest.setHeader('origin', apiProxyTarget)
            } catch {
              // Preserve malformed or foreign origins so the backend rejects them.
            }
          })
        },
      },
    },
  },
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    css: true,
    coverage: {
      reporter: ['text', 'html'],
      include: ['src/**/*.{ts,tsx}'],
    },
  },
})
