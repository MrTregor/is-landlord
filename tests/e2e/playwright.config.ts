// Сквозные сценарии (docs/spec/16 Тестирование.md, раздел 4) на стеке demo: make e2e.
// Стек — docker-compose.prod.yml --profile demo с переменными tests/e2e/e2e.env (https://localhost, локальный
// сертификат Caddy). Сценарии меняют общие данные и дату системы — выполняются по одному, по порядку файлов.
import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './specs',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 180_000,
  expect: { timeout: 15_000 },
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.BASE_URL ?? 'https://localhost',
    ignoreHTTPSErrors: true,
    locale: 'ru-RU',
    timezoneId: 'Europe/Moscow',
    acceptDownloads: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    ...devices['Desktop Chrome'],
    viewport: { width: 1440, height: 900 },
  },
})
