import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  workers: 1,
  timeout: 45000,
  use: { baseURL: process.env.BASE_URL || 'http://localhost:8090', channel: process.env.PLAYWRIGHT_CHANNEL, trace: 'retain-on-failure' },
})
