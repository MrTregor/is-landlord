import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { routes } from './router'
import { installGuards } from './guards'
import { useSessionStore } from './session'

vi.mock('@/api/client', () => ({
  api: { GET: vi.fn(), POST: vi.fn() },
  unwrap: vi.fn().mockRejectedValue(new Error('нет сессии')),
  setCsrfToken: vi.fn(),
  setUnauthorizedHandler: vi.fn(),
}))

function makeRouter() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  installGuards(router)
  return router
}

function loginAs(role: 'owner' | 'tenant') {
  const s = useSessionStore()
  s.user = {
    id: '0199f1b2-7c3e-7a10-9d4e-3f5a6b7c8d90',
    email: 'u@landlord.local',
    role,
    status: 'active',
    displayName: 'Пользователь',
    tenantId: null,
    lastLoginAt: null,
    createdAt: '2026-10-16T00:00:00Z',
  }
  s.loaded = true
}

describe('охрана маршрутов', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('без сессии отправляет на вход с адресом возврата', async () => {
    const router = makeRouter()
    await router.push('/contracts?status=active')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.next).toBe('/contracts?status=active')
  })

  it('страница входа открывается без сессии', async () => {
    const router = makeRouter()
    await router.push('/login')
    expect(router.currentRoute.value.name).toBe('login')
  })

  it('собственника из кабинета возвращает на сводку', async () => {
    loginAs('owner')
    const router = makeRouter()
    await router.push('/cabinet/charges')
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('нанимателя со страниц собственника отправляет в кабинет', async () => {
    loginAs('tenant')
    const router = makeRouter()
    await router.push('/payments')
    expect(router.currentRoute.value.path).toBe('/cabinet')
  })

  it('неизвестный путь — страница «не найдена»', async () => {
    const router = makeRouter()
    await router.push('/no/such/page')
    expect(router.currentRoute.value.name).toBe('not-found')
  })
})
