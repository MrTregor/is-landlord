// Нагрузочный тест NFR-Perf-01 (docs/spec/16 Тестирование.md, раздел 5): 20 виртуальных пользователей, 5 минут,
// пауза между запросами 1–3 с. 15 собственников по кругу: сводка, начисления, действующие договоры, карточка
// договора, платежи за период, задолженность; 5 нанимателей: сводка, начисления, платежи кабинета.
// Данные — cli seed --demo --load. Запуск — make load (k6 в сетевом пространстве контейнера web, https://localhost).
// Пороги: p(95) < 2000 мс для каждого запроса, доля ошибок < 1 %.
import http from 'k6/http'
import exec from 'k6/execution'
import { check, fail, sleep } from 'k6'

const BASE = (__ENV.BASE_URL || 'https://localhost') + '/api/v1'
const OWNER = { email: 'owner@landlord.local', password: 'Demo-2026-owner' }
const TENANTS = ['kuznetsov@landlord.local', 'romashka@landlord.local', 'smirnova@landlord.local']
const TENANT_PASSWORD = 'Demo-2026-tenant'
const OWNER_VUS = 15
const TENANT_VUS = 5
const DURATION = __ENV.DURATION || '5m'

const ownerRequests = ['dashboard', 'charges', 'contracts', 'contract', 'payments', 'debts']
const tenantRequests = ['cabinet_summary', 'cabinet_charges', 'cabinet_payments']
const thresholds = { http_req_failed: ['rate<0.01'], 'http_req_duration{scenario:owners}': ['p(95)<2000'] }
for (const name of [...ownerRequests, ...tenantRequests]) {
  thresholds[`http_req_duration{name:${name}}`] = ['p(95)<2000']
}

export const options = {
  insecureSkipTLSVerify: true, // локальный сертификат Caddy для localhost
  thresholds,
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
  scenarios: {
    owners: { executor: 'constant-vus', vus: OWNER_VUS, duration: DURATION, exec: 'owner' },
    tenants: { executor: 'constant-vus', vus: TENANT_VUS, duration: DURATION, exec: 'tenant' },
  },
}

// Отдельный сеанс на каждого виртуального пользователя: ограничение частоты считается по сеансу (300 в минуту).
function login(email, password) {
  const res = http.post(`${BASE}/auth/login`, JSON.stringify({ email, password }), {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'login' },
  })
  if (res.status !== 200 || !res.cookies.ll_session) fail(`вход ${email}: ${res.status} ${res.body}`)
  return res.cookies.ll_session[0].value
}

const params = (session, name) => ({ cookies: { ll_session: { value: session, replace: true } }, tags: { name } })

function getJSON(url, session, name) {
  const res = http.get(url, params(session, name))
  if (res.status !== 200) fail(`${url}: ${res.status}`)
  return res.json()
}

const iso = (d) => d.toISOString().slice(0, 10)

export function setup() {
  const owners = []
  for (let i = 0; i < OWNER_VUS; i++) owners.push(login(OWNER.email, OWNER.password))
  const contracts = getJSON(`${BASE}/contracts?status=active&pageSize=100`, owners[0], 'setup').items.map((c) => c.id)
  if (contracts.length < 100) fail(`действующих договоров ${contracts.length} — загрузите данные: cli seed --demo --load`)
  const tenants = []
  for (let i = 0; i < TENANT_VUS; i++) {
    const session = login(TENANTS[i % TENANTS.length], TENANT_PASSWORD)
    tenants.push({ session, contractId: getJSON(`${BASE}/cabinet/summary`, session, 'setup').contractId })
  }
  const today = getJSON(`${BASE}/dev/clock`, owners[0], 'setup').today
  const from = new Date(today)
  from.setUTCMonth(from.getUTCMonth() - 3, 1)
  return { owners, tenants, contracts, from: iso(from), to: today }
}

function pause() {
  sleep(1 + Math.random() * 2)
}

export function owner(data) {
  const session = data.owners[exec.vu.idInTest % OWNER_VUS]
  const contract = data.contracts[Math.floor(Math.random() * data.contracts.length)]
  const urls = {
    dashboard: `${BASE}/dashboard`,
    charges: `${BASE}/charges?page=1`,
    contracts: `${BASE}/contracts?status=active`,
    contract: `${BASE}/contracts/${contract}`,
    payments: `${BASE}/payments?from=${data.from}&to=${data.to}`,
    debts: `${BASE}/reports/debts`,
  }
  for (const name of ownerRequests) {
    const res = http.get(urls[name], params(session, name))
    check(res, { [`${name}: 200`]: (r) => r.status === 200 })
    pause()
  }
}

export function tenant(data) {
  const t = data.tenants[exec.vu.idInTest % TENANT_VUS]
  const urls = {
    cabinet_summary: `${BASE}/cabinet/summary`,
    cabinet_charges: `${BASE}/cabinet/charges?contractId=${t.contractId}`,
    cabinet_payments: `${BASE}/cabinet/payments?contractId=${t.contractId}`,
  }
  for (const name of tenantRequests) {
    const res = http.get(urls[name], params(t.session, name))
    check(res, { [`${name}: 200`]: (r) => r.status === 200 })
    pause()
  }
}
