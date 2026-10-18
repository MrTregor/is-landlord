// E2E-1: собственник — вход → объект → наниматель → договор через мастер → ввод в действие → начисления видны,
// документ договора открывается (FR-01.1, FR-02.1, FR-03.1, FR-05.1, FR-04.1).
import { expect, test } from '@playwright/test'
import { choose, fillDate, loginOwner, press, reseed, ru, systemToday, visibleText, wizardNext } from '../support/stack'

test.beforeAll(async ({ request }) => reseed(request))

test('E2E-1 собственник оформляет договор через мастер (FR-03.1, FR-05.1)', async ({ page }) => {
  await loginOwner(page)
  const today = await systemToday(page.request)
  const monthStart = ru(today.slice(0, 8) + '01')

  // Объект
  await page.goto('/properties/new')
  // ТК-03: у нежилого объекта режим чека по умолчанию — «Облачная касса» (FR-01.4)
  await page.getByRole('button', { name: 'Нежилой', exact: true }).click()
  await expect(page.locator('#p-fiscal')).toContainText('Облачная касса')
  await page.getByRole('button', { name: 'Жилой', exact: true }).click()
  await expect(page.locator('#p-fiscal')).toContainText('Чек не требуется')
  await page.locator('#p-code').fill('Студия 5')
  await page.locator('#p-address').fill('г. Руза, ул. Солнечная, д. 7, кв. 3')
  await page.locator('#p-area').fill('22,4')
  await page.locator('#p-rent').fill('25 000')
  await fillDate(page, 'p-since', monthStart)
  await page.getByRole('button', { name: 'Сохранить' }).click()
  await expect(page).toHaveURL(/\/properties\/[0-9a-f-]{36}$/)
  await expect(visibleText(page, 'Студия 5')).toBeVisible()

  // Наниматель
  await page.goto('/tenants/new')
  await page.locator('#t-last').fill('Петров')
  await page.locator('#t-first').fill('Пётр')
  await page.locator('#t-middle').fill('Петрович')
  await fillDate(page, 't-birth', '12.04.1990')
  await page.locator('#t-docnum').fill('4510 123456')
  await page.locator('#t-issued').fill('ГУ МВД России по г. Москве')
  await fillDate(page, 't-issuedon', '20.04.2010')
  await page.locator('#t-division').fill('770-001')
  await page.locator('#t-regaddr').fill('г. Москва, ул. Примерная, д. 10, кв. 5')
  await page.locator('#t-email').fill('petrov@landlord.local')
  await page.getByRole('button', { name: 'Сохранить' }).click()
  await expect(page).toHaveURL(/\/tenants\/[0-9a-f-]{36}$/)

  // Договор через мастер
  await page.goto('/contracts/new')
  await choose(page, 'w-property', /Студия 5/)
  await choose(page, 'w-tenant', /Петров/, 'Петров')
  await wizardNext(page, page.locator('#w-start'))
  await fillDate(page, 'w-start', monthStart)
  await page.getByRole('button', { name: '11 месяцев' }).click()
  await page.locator('#w-rent').fill('25 000')
  await page.locator('#w-rent').blur()
  await page.locator('#w-deposit').fill('25 000')
  await page.locator('#w-deposit').blur()
  await wizardNext(page, page.locator('#w-pen'))
  await press(page, 'Включены в плату')
  await wizardNext(page, page.locator('#w-special'))
  const project = page.getByRole('button', { name: 'Сформировать проект договора (PDF)' })
  await wizardNext(page, project)
  await expect(visibleText(page, 'Петров П. П.')).toBeVisible()
  await page.getByRole('button', { name: 'Подписан — ввести в действие' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: 'Ввести в действие' }).click()
  await expect(page).toHaveURL(/\/contracts\/[0-9a-f-]{36}$/)
  await expect(visibleText(page, 'Действует')).toBeVisible()

  // Начисления: плата за текущий месяц
  await page.getByRole('tab', { name: 'Начисления' }).click()
  await expect(visibleText(page, /Плата за наём/)).toBeVisible()
  await expect(visibleText(page, '25 000,00 ₽')).toBeVisible()

  // Документ договора открывается (PDF)
  await page.getByRole('tab', { name: 'Документы' }).click()
  const pdfLink = page.getByRole('link', { name: /Открыть PDF/ }).first()
  if (!(await pdfLink.isVisible().catch(() => false))) {
    const popup = page.waitForEvent('popup')
    await page.getByRole('button', { name: 'Сформировать договор заново' }).click()
    await (await popup).close()
  }
  await expect(pdfLink).toBeVisible()
  const href = await pdfLink.getAttribute('href')
  const pdf = await page.request.get(href!)
  expect(pdf.ok()).toBeTruthy()
  expect(pdf.headers()['content-type']).toContain('application/pdf')
  expect((await pdf.body()).subarray(0, 4).toString()).toBe('%PDF')
})
