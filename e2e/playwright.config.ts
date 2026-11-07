import { defineConfig, devices } from '@playwright/test'
export default defineConfig({
  testDir: '.',
  timeout: 120_000,
  workers: 1,
  reporter: [['list']],
  use: { ...devices['iPhone 13'], baseURL: process.env.BASE || 'http://127.0.0.1:8099', locale: 'ru-RU', timezoneId: 'Europe/Moscow', trace: 'retain-on-failure' },
})
