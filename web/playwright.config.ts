import { defineConfig, devices } from '@playwright/test'

const inheritedEnvironment = Object.fromEntries(Object.entries(process.env).filter((entry): entry is [string, string] => typeof entry[1] === 'string'))
const backendPort = process.env.MULTISPEED_E2E_BACKEND_PORT ?? '18787'
const backendURL = `http://127.0.0.1:${backendPort}`

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  globalTeardown: './e2e/global-teardown.ts',
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list']],
  use: {
    baseURL: 'http://127.0.0.1:5173',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: 'node ./e2e/start-backend.mjs',
      url: `${backendURL}/api/v1/readyz`,
      env: { ...inheritedEnvironment, MULTISPEED_E2E_BACKEND_PORT: backendPort },
      reuseExistingServer: false,
      timeout: 300_000,
    },
    {
      command: 'npm run dev -- --host 127.0.0.1',
      url: 'http://127.0.0.1:5173',
      env: { ...inheritedEnvironment, MULTISPEED_API_PROXY_TARGET: backendURL },
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
})
