// E2E-4: наниматель создаёт заявку с фото → собственник: «В работе», затрата «по вине нанимателя» с удержанием
// из залога, «Выполнена» → наниматель подтверждает → остаток залога уменьшился (FR-08.1, FR-05.6, FR-05.2).
import { expect, test, type Page } from '@playwright/test'
import { loginOwner, loginTenant, parseRu, press, reseed, choose, visibleText } from '../support/stack'

// Изображение 1×1 PNG — «фото» неисправности.
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
)
const TITLE = 'Не закрывается окно на кухне'

async function depositBalance(page: Page): Promise<number> {
  await page.goto('/cabinet')
  const text = await visibleText(page, /остаток/).innerText()
  return parseRu(text.match(/остаток\s*([\d\s ,]+)/)![1])
}

test.beforeAll(async ({ request }) => reseed(request))

test('E2E-4 заявка с фото → затрата с удержанием из залога → подтверждение (FR-08.1, FR-05.6)', async ({
  browser,
}) => {
  const tenant = await browser.newPage()
  await loginTenant(tenant, 'kuznetsov@landlord.local')
  const before = await depositBalance(tenant)

  // Заявка нанимателя с фото
  await tenant.goto('/cabinet/requests/new')
  await tenant.getByRole('radio', { name: 'Двери и окна' }).or(tenant.getByRole('radio').first()).first().click()
  await tenant.locator('#cr-title').fill(TITLE)
  await tenant.locator('#cr-desc').fill('Створка не прижимается, из окна дует.')
  await tenant.locator('input[type=file]').setInputFiles({ name: 'okno.png', mimeType: 'image/png', buffer: PNG })
  await expect(tenant.getByRole('img', { name: 'okno.png' })).toBeVisible()
  await tenant.getByRole('button', { name: 'Отправить' }).click()
  await expect(tenant).toHaveURL(/\/cabinet\/requests\/[0-9a-f-]{36}/)

  // Собственник: в работу, затрата по вине нанимателя с удержанием из залога, выполнена
  const owner = await browser.newPage()
  await loginOwner(owner)
  await owner.goto('/requests')
  await owner.getByText(TITLE).first().click()
  await expect(owner).toHaveURL(/\/requests\/[0-9a-f-]{36}/)
  await expect(owner.getByRole('img').first()).toBeVisible()
  await owner.getByRole('button', { name: 'Взять в работу' }).click()
  await expect(visibleText(owner, 'В работе')).toBeVisible()
  await owner.locator('#rc-fault').check()
  await owner.getByRole('button', { name: 'Добавить затрату' }).click()
  const dialog = owner.getByRole('dialog')
  await choose(owner, 'ex-category', /.+/)
  await dialog.locator('#ex-amount').fill('4 200')
  await dialog.locator('#ex-desc').fill('Регулировка окна')
  await press(owner, 'Удержать из залога')
  await dialog.getByRole('button', { name: 'Добавить' }).click()
  await expect(dialog).toBeHidden()
  await expect(visibleText(owner, '4 200,00 ₽')).toBeVisible()
  await owner.getByRole('button', { name: 'Выполнена' }).click()
  await owner.locator('#rc-move').fill('Окно отрегулировано, уплотнитель заменён')
  await owner.getByRole('dialog').getByRole('button', { name: 'Сохранить' }).click()
  await expect(visibleText(owner, 'Выполнена')).toBeVisible()

  // Наниматель подтверждает выполнение; остаток залога меньше на 4 200 ₽
  await tenant.goto('/cabinet/requests')
  await tenant.getByText(TITLE).first().click()
  await tenant.getByRole('button', { name: 'Подтверждаю выполнение' }).click()
  await expect(visibleText(tenant, 'Закрыта')).toBeVisible()
  expect(await depositBalance(tenant)).toBeCloseTo(before - 4200, 2)
})
