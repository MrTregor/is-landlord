// E2E-6: собственник загружает выписку CSV → разбирает неопознанный платёж привязкой к договору (FR-07.2, FR-07.3).
import { expect, test } from '@playwright/test'
import { choose, loginOwner, press, reseed, ru, systemToday, visibleText } from '../support/stack'

test.beforeAll(async ({ request }) => reseed(request))

test('E2E-6 выписка CSV → неопознанный платёж привязан к договору (FR-07.2, FR-07.3)', async ({ page }) => {
  await loginOwner(page)
  const today = ru(await systemToday(page.request))
  // Формат CSV — 08 разд. 7 (образец — backend/testdata/statement.csv). Плательщик не похож ни на кого из нанимателей.
  const csv =
    'Дата;Сумма;Плательщик;ИНН плательщика;Счёт плательщика;Назначение;Номер документа\r\n' +
    `${today};15 000,00;Сидоров Иван Ильич;;40817810900000000099;перевод по просьбе;9001\r\n`

  await page.goto('/reconciliation')
  await page.getByRole('tab', { name: 'Выписки банка' }).click()
  await page.locator('input[type=file]').setInputFiles({
    name: 'vypiska.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from(csv, 'utf8'),
  })
  // После загрузки открывается выписка: строка «Не опознан»
  await expect(page).toHaveURL(/\/reconciliation\/statements\/[0-9a-f-]{36}/)
  await expect(page.getByRole('row').filter({ hasText: 'Сидоров' }).getByText('Не опознан')).toBeVisible()

  // Расхождение «неопознанный платёж» — привязать к договору Кузнецова
  await page.getByRole('link', { name: 'Разобрать неопознанные платежи' }).click()
  const row = page.getByRole('row').filter({ hasText: 'Сидоров' })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: 'Разобрать' }).click()
  const dialog = page.getByRole('dialog')
  await press(page, 'Привязать к договору')
  await choose(page, 'rd-contract', /2026-002/)
  await dialog.getByRole('button', { name: 'Сохранить' }).click()
  await expect(dialog).toBeHidden()
  await expect(row).toBeHidden()

  // Платёж из выписки появился в списке платежей
  await page.goto('/payments')
  await expect(visibleText(page, '15 000,00 ₽')).toBeVisible()
})
