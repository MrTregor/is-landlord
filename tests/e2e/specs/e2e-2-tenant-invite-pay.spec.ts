// E2E-2: наниматель принимает приглашение (ссылка из Mailpit) → ознакомление с политикой → главная → «Оплатить»
// → страница эмулятора Т-Банка → «Оплатить картой» → «Оплата прошла» → квитанция в списке платежей
// (FR-13.2, FR-06.1, FR-06.3, NFR-Sec-03).
import { expect, test } from '@playwright/test'
import { mailMessages, mailText, reseed, visibleText } from '../support/stack'

test.beforeAll(async ({ request }) => reseed(request))

test('E2E-2 наниматель по приглашению оплачивает онлайн (FR-13.2, FR-06.1)', async ({ page, request }) => {
  // Письмо-приглашение Лебедеву (отправляет worker после загрузки демо-данных).
  let invite = ''
  await expect(async () => {
    const msgs = await mailMessages(request, 'to:lebedev@landlord.local')
    expect(msgs.length).toBeGreaterThan(0)
    const text = await mailText(request, msgs[0].ID)
    const m = text.match(/https?:\/\/[^\s"'<>]+\/invite\/[A-Za-z0-9_-]+/)
    expect(m).not.toBeNull()
    invite = m![0]
  }).toPass({ timeout: 60_000, intervals: [2_000] })

  // Пароль и ознакомление с политикой
  await page.goto(new URL(invite).pathname)
  await page.locator('#invite-password').fill('Lebedev-2026-pass')
  await page.locator('#invite-repeat').fill('Lebedev-2026-pass')
  await page.locator('#invite-agree').check()
  await page.getByRole('button', { name: 'Создать пароль и войти' }).click()
  await expect(page).toHaveURL(/\/cabinet$/)
  await expect(visibleText(page, 'К оплате')).toBeVisible()

  // Онлайн-оплата через эмулятор
  await page.getByRole('button', { name: 'Оплатить картой или СБП' }).click()
  await expect(page).toHaveURL(/localhost:8081\/pay\//)
  await page.getByRole('button', { name: /^Оплатить / }).click()

  // Возврат в кабинет: результат оплаты
  await expect(page).toHaveURL(/\/cabinet\/payments\/[0-9a-f-]{36}\/result/, { timeout: 30_000 })
  await expect(page.getByRole('heading', { name: 'Оплата прошла' })).toBeVisible({ timeout: 60_000 })

  // Квитанция в списке платежей
  await page.goto('/cabinet/charges')
  await page.getByRole('tab', { name: /Платежи/ }).click()
  const receipt = page.getByRole('link', { name: /Квитанция/ }).first()
  await expect(async () => {
    await page.reload()
    await page.getByRole('tab', { name: /Платежи/ }).click()
    await expect(receipt).toBeVisible({ timeout: 3_000 })
  }).toPass({ timeout: 60_000 })
  const pdf = await page.request.get((await receipt.getAttribute('href'))!)
  expect(pdf.ok()).toBeTruthy()
  expect(pdf.headers()['content-type']).toContain('application/pdf')
})
