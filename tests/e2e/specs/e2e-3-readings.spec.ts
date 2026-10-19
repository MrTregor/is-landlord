// E2E-3: наниматель передаёт показания → собственник принимает → у нанимателя начисление ЖКУ (FR-05.5, FR-13.1).
// Окно приёма показаний открывается с readings_reminder_day (20-е) — дата системы сдвигается на 25-е число.
import { expect, test } from '@playwright/test'
import { loginOwner, loginTenant, parseRu, reseed, setSystemDate, systemToday, visibleText } from '../support/stack'

test.beforeAll(async ({ request }) => reseed(request))

test('E2E-3 показания нанимателя → принятие → начисление ЖКУ (FR-05.5)', async ({ browser }) => {
  const owner = await browser.newPage()
  await loginOwner(owner)
  const today = await systemToday(owner.request)
  const day = Number(today.slice(8))
  if (day > 5 && day < 20) {
    // С 6-го по 19-е окно закрыто (05 разд. 6.2, решение № 99).
    await setSystemDate(owner, today.slice(0, 8) + '25') // сеанс не завершается: сроки — по реальному времени (№ 112)
  }

  // Наниматель Студии 1 передаёт показания всех счётчиков за текущий месяц.
  const tenant = await browser.newPage()
  await loginTenant(tenant, 'smirnova@landlord.local')
  await tenant.goto('/cabinet/meters')
  const cards = tenant.locator('section.card').filter({ has: tenant.locator('input[id^="rd-"]:not([disabled])') })
  await expect(cards.first()).toBeVisible()
  const n = await cards.count()
  expect(n).toBe(3)
  for (let i = 0; i < n; i++) {
    const card = cards.nth(i)
    // «Предыдущее» — последнее принятое; показания прошлого месяца ещё на проверке — новое берём с запасом.
    const hint = await card.locator('.hint', { hasText: 'Предыдущее' }).innerText()
    const prev = parseRu(hint.match(/Предыдущее:\s*([\d\s ,]+)/)![1])
    const next = prev + (prev > 1000 ? 500 : 25)
    await card.locator('input[id^="rd-"]').fill(next.toFixed(3).replace('.', ','))
  }
  await tenant.getByRole('button', { name: 'Передать показания' }).click()
  await expect(visibleText(tenant, 'На проверке')).toBeVisible()

  // Собственник принимает все показания на проверке (прошлого и текущего месяца).
  await owner.goto('/readings')
  const accept = owner.getByRole('button', { name: 'Принять', exact: true })
  await expect(accept.first()).toBeVisible()
  while ((await accept.count()) > 0) {
    const before = await accept.count()
    await accept.first().click()
    await expect(accept).toHaveCount(before - 1)
  }

  // У нанимателя — начисление «Компенсация ЖКУ».
  await tenant.goto('/cabinet/charges')
  await expect(visibleText(tenant, /Компенсация ЖКУ|ЖКУ за/)).toBeVisible()
})
