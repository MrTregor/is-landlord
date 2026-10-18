// E2E-7: демо-режим — дата системы → следующий месяц, «Выполнить ежедневные задачи» → новые начисления, пени по
// просрочке, письма-напоминания в Mailpit (NFR-Comp-02 демо, FR-05.1, FR-05.3, FR-10.1).
// Дата — 7-е число следующего месяца: за 3 дня (remind_before_due_days) до срока оплаты Смирновой (10-е).
import { expect, test } from '@playwright/test'
import {
  fillDate,
  loginOwner,
  mailMessages,
  nextMonthStart,
  reseed,
  ru,
  systemToday,
  visibleText,
} from '../support/stack'

const MONTHS = ['январь', 'февраль', 'март', 'апрель', 'май', 'июнь', 'июль', 'август', 'сентябрь', 'октябрь', 'ноябрь', 'декабрь']

test.beforeAll(async ({ request }) => reseed(request))

test('E2E-7 сдвиг даты и ежедневные задачи: начисления, пени, напоминания (FR-05.1, FR-05.3, FR-10.1)', async ({
  page,
  request,
}) => {
  await loginOwner(page)
  const target = nextMonthStart(await systemToday(page.request)).slice(0, 8) + '07'
  const month = MONTHS[Number(target.slice(5, 7)) - 1]

  await page.goto('/settings/service')
  await fillDate(page, 'demo-date', ru(target))
  const applied = page.waitForResponse((r) => r.url().endsWith('/api/v1/dev/clock') && r.request().method() === 'PUT')
  await page.getByRole('button', { name: 'Применить' }).click()
  expect((await applied).ok()).toBeTruthy()
  // Сеанс считается по часам системы: после сдвига на дни вперёд он истекает — вход заново.
  await page.context().clearCookies()
  await loginOwner(page)
  await page.goto('/settings/service')
  await expect(visibleText(page, `дата системы: ${ru(target)}`)).toBeVisible()

  await page.getByRole('button', { name: 'Выполнить ежедневные задачи' }).click()
  const results = page.locator('.daily')
  await expect(results.getByRole('row').filter({ hasText: 'Начисления платы' })).toContainText('Успешно', {
    timeout: 120_000,
  })
  await expect(results).not.toContainText('С ошибками')

  // Новые начисления за следующий месяц и пени по просрочке
  await page.goto('/charges')
  await expect(visibleText(page, new RegExp(`Плата за наём.*${month}`, 'i'))).toBeVisible()
  await expect(visibleText(page, /Пени/)).toBeVisible()

  // Письма-напоминания (worker отправляет их из очереди в Mailpit)
  await expect(async () => {
    const reminders = await mailMessages(request, 'subject:"Напоминание об оплате"')
    const overdue = await mailMessages(request, 'subject:"Просрочка оплаты"')
    expect(reminders.length + overdue.length).toBeGreaterThan(0)
  }).toPass({ timeout: 60_000, intervals: [2_000] })
})
