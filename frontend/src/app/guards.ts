// Охрана маршрутов (13 Фронтенд, раздел 4): без сессии → /login?next=…; собственник в кабинете → /;
// наниматель на страницах собственника → /cabinet.
import type { Router } from 'vue-router'
import { setUnauthorizedHandler } from '@/api/client'
import { APP_NAME } from './constants'
import { useSessionStore } from './session'

export function homeFor(role: string | null): string {
  return role === 'tenant' ? '/cabinet' : '/'
}

export function installGuards(router: Router): void {
  router.beforeEach(async (to) => {
    const session = useSessionStore()
    await session.ensureLoaded()
    const access = to.meta.access ?? 'owner'

    if (access === 'public') {
      if (to.name === 'login' && session.isAuthenticated) return homeFor(session.role)
      return true
    }
    if (!session.isAuthenticated) {
      return { path: '/login', query: { next: to.fullPath } }
    }
    if (access !== session.role) return homeFor(session.role)
    return true
  })

  router.afterEach((to) => {
    document.title = to.meta.title ? `${to.meta.title} — ${APP_NAME}` : APP_NAME
  })

  setUnauthorizedHandler(() => {
    const session = useSessionStore()
    session.reset()
    const current = router.currentRoute.value
    if (current.meta.access !== 'public') {
      void router.push({ path: '/login', query: { next: current.fullPath } })
    }
  })
}
