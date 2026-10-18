// E2E-5: собственник — отчёт «Налоговая база» за год → «Выгрузить в Excel» → файл скачан (имя, размер > 0);
// засекается время от входа до выгрузки — для цели ТЗ «формирование налоговой базы» (FR-11.3, FR-11.4, 16 разд. 9).
import { statSync, writeFileSync, mkdirSync } from 'node:fs'
import { expect, test } from '@playwright/test'
import { loginOwner, reseed } from '../support/stack'

test.beforeAll(async ({ request }) => reseed(request))

test('E2E-5 налоговая база за год выгружается в Excel (FR-11.3, FR-11.4)', async ({ page }, info) => {
  const started = Date.now()
  await loginOwner(page)
  await page.getByRole('link', { name: 'Отчёты' }).click()
  await page.getByRole('link', { name: /Налоговая база/ }).click()
  await expect(page).toHaveURL(/\/reports\/tax-base/)
  await page.getByRole('button', { name: 'Построить' }).click()
  await expect(page.getByText(/Налог/).filter({ visible: true }).nth(1)).toBeVisible()

  const download = page.waitForEvent('download')
  await page.getByRole('link', { name: 'Выгрузить в Excel' }).click()
  const file = await download
  const path = info.outputPath(file.suggestedFilename())
  await file.saveAs(path)
  const seconds = (Date.now() - started) / 1000

  expect(file.suggestedFilename()).toMatch(/\.xlsx$/)
  expect(statSync(path).size).toBeGreaterThan(0)
  info.annotations.push({ type: 'время от входа до выгрузки, с', description: seconds.toFixed(1) })
  mkdirSync('test-results', { recursive: true })
  writeFileSync(
    'test-results/e2e-5-timing.json',
    JSON.stringify({ file: file.suggestedFilename(), bytes: statSync(path).size, seconds }, null, 2),
  )
})
