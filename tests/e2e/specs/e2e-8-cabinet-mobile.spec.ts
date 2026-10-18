// E2E-8: телефон (390×844) — все страницы кабинета без горизонтальной прокрутки:
// document.documentElement.scrollWidth <= 390 (NFR-Usab-01, 15 Экраны нанимателя).
import { expect, test, type Page } from '@playwright/test'
import { loginTenant, reseed } from '../support/stack'

test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })

test.beforeAll(async ({ request }) => reseed(request))

async function noHorizontalScroll(page: Page, path: string): Promise<void> {
  await page.goto(path)
  await page.waitForLoadState('networkidle')
  await expect(page.locator('main, .cabinet, body').first()).toBeVisible()
  const width = await page.evaluate(() => document.documentElement.scrollWidth)
  expect.soft(width, `${path}: scrollWidth`).toBeLessThanOrEqual(390)
}

test('E2E-8 кабинет на телефоне без горизонтальной прокрутки (NFR-Usab-01)', async ({ page }) => {
  await loginTenant(page, 'smirnova@landlord.local')

  const summary = await (await page.request.get('/api/v1/cabinet/summary')).json()
  const contractId: string = summary.contractId
  const payments = await (await page.request.get(`/api/v1/cabinet/payments?contractId=${contractId}`)).json()
  const requests = await (await page.request.get(`/api/v1/cabinet/requests?contractId=${contractId}`)).json()
  expect(payments.length).toBeGreaterThan(0)
  expect(requests.length).toBeGreaterThan(0)

  for (const path of [
    '/cabinet',
    '/cabinet/charges',
    '/cabinet/charges?tab=payments',
    `/cabinet/payments/${payments[0].id}/result`,
    '/cabinet/meters',
    '/cabinet/requests',
    '/cabinet/requests/new',
    `/cabinet/requests/${requests[0].id}`,
    '/cabinet/contract',
    '/cabinet/profile',
    '/privacy',
  ]) {
    await noHorizontalScroll(page, path)
  }
})
