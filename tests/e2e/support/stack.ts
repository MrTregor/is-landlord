// Стек demo: загрузка демо-данных, вход, дата системы, почта Mailpit.
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { expect, type APIRequestContext, type Locator, type Page } from '@playwright/test'

const root = fileURLToPath(new URL('../../..', import.meta.url))
const compose = ['compose', '-f', 'docker-compose.prod.yml', '--env-file', 'tests/e2e/e2e.env']

export const OWNER = { email: 'owner@landlord.local', password: 'Demo-2026-owner' }
export const TENANT_PASSWORD = 'Demo-2026-tenant'
export const MAILPIT = process.env.MAILPIT_URL ?? 'http://localhost:8025'
const MAILPIT_AUTH = 'Basic ' + Buffer.from('demo:e2e-mailpit').toString('base64')

/** Команда cli в контейнере server. */
export function cli(...args: string[]): string {
  return execFileSync('docker', [...compose, 'exec', '-T', 'server', '/app/cli', ...args], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, MSYS_NO_PATHCONV: '1' },
    timeout: 300_000,
  })
}

/** Очистить почту Mailpit, бизнес-данные и загрузить демо (17 Демо-данные). */
export async function reseed(request: APIRequestContext): Promise<void> {
  await request.delete(`${MAILPIT}/api/v1/messages`, { headers: { Authorization: MAILPIT_AUTH } })
  cli('seed', '--demo')
}

export async function login(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/login')
  await page.locator('#login-email').fill(email)
  await page.locator('#login-password').fill(password)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

export const loginOwner = (page: Page) => login(page, OWNER.email, OWNER.password)
export const loginTenant = (page: Page, email: string) => login(page, email, TENANT_PASSWORD)

/** Письма Mailpit (новые первыми). */
export async function mailMessages(
  request: APIRequestContext,
  query = '',
): Promise<{ ID: string; Subject: string; To: { Address: string }[] }[]> {
  const url = query
    ? `${MAILPIT}/api/v1/search?query=${encodeURIComponent(query)}`
    : `${MAILPIT}/api/v1/messages`
  const res = await request.get(url, { headers: { Authorization: MAILPIT_AUTH } })
  expect(res.ok()).toBeTruthy()
  return (await res.json()).messages ?? []
}

export async function mailText(request: APIRequestContext, id: string): Promise<string> {
  const res = await request.get(`${MAILPIT}/api/v1/message/${id}`, { headers: { Authorization: MAILPIT_AUTH } })
  expect(res.ok()).toBeTruthy()
  const m = await res.json()
  return `${m.Text ?? ''}\n${m.HTML ?? ''}`
}

/** Первое число следующего месяца относительно даты YYYY-MM-DD. */
export function nextMonthStart(isoDate: string): string {
  const [y, m] = isoDate.split('-').map(Number)
  return m === 12 ? `${y + 1}-01-01` : `${y}-${String(m + 1).padStart(2, '0')}-01`
}

/** Ввод даты в DatePicker (дд.мм.гггг). */
export async function fillDate(page: Page, inputId: string, ddmmyyyy: string): Promise<void> {
  const input = page.locator(`#${inputId}`)
  await input.fill(ddmmyyyy)
  await input.press('Escape')
}

/** Выбор значения в Select PrimeVue по id элемента. */
export async function choose(page: Page, id: string, option: string | RegExp, filter?: string): Promise<void> {
  await page.locator(`#${id}`).click()
  if (filter) await page.getByRole('searchbox').or(page.locator('.p-select-filter')).first().fill(filter)
  await page.getByRole('option', { name: option }).first().click()
}

/** «Сегодня» системы (дата демо-режима) — YYYY-MM-DD. */
export async function systemToday(request: APIRequestContext): Promise<string> {
  const res = await request.get('/api/v1/dev/clock')
  expect(res.ok()).toBeTruthy()
  return (await res.json()).today
}

export const ru = (iso: string) => iso.split('-').reverse().join('.')

/** «Далее» в мастере: нажимать, пока не появится элемент следующего шага (панели сменяются с анимацией). */
export async function wizardNext(page: Page, nextStep: Locator): Promise<void> {
  await expect(async () => {
    if (await nextStep.isVisible()) return
    await page.getByRole('button', { name: 'Далее' }).filter({ visible: true }).last().click({ timeout: 2_000 })
    await expect(nextStep).toBeVisible({ timeout: 3_000 })
  }).toPass({ timeout: 30_000 })
}

/** Нажать кнопку-переключатель (SelectButton), если она ещё не выбрана. */
export async function press(page: Page, name: string): Promise<void> {
  const b = page.getByRole('button', { name, exact: true }).filter({ visible: true }).first()
  if ((await b.getAttribute('aria-pressed')) !== 'true') await b.click()
}

/** Видимый текст на странице (скрытые копии — в списках выбора, неактивных вкладках — не учитываются). */
export const visibleText = (page: Page, text: string | RegExp) =>
  page.getByText(text).filter({ visible: true }).first()

/** Изменяющий запрос API от имени вошедшего пользователя страницы (CSRF из /auth/me). */
export async function apiPost(page: Page, path: string, data?: unknown, method: 'POST' | 'PUT' = 'POST') {
  const me = await page.request.get('/api/v1/auth/me')
  expect(me.ok()).toBeTruthy()
  const { csrfToken } = await me.json()
  const res = await page.request.fetch(`/api/v1${path}`, { method, data, headers: { 'X-CSRF-Token': csrfToken } })
  expect(res.ok(), `${path}: ${res.status()} ${await res.text()}`).toBeTruthy()
  return res.json()
}

/** Демо-режим: дата системы (собственник). */
export const setSystemDate = (page: Page, iso: string) => apiPost(page, '/dev/clock', { today: iso }, 'PUT')

/** Число из записи «1 234,567». */
export const parseRu = (s: string) => Number(s.replace(/[\s\u00a0]/g, '').replace(',', '.'))
