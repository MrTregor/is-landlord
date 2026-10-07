// Сессия пользователя (Pinia): пользователь, CSRF-токен, роль. Данные сервера — только через vue-query.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, setCsrfToken, unwrap } from '@/api/client'
import type { components } from '@/api/schema'

type AuthResponse = components['schemas']['AuthResponse']
type User = components['schemas']['UserDTO']

export const useSessionStore = defineStore('session', () => {
  const user = ref<User | null>(null)
  const mustAcceptPolicy = ref(false)
  const policyVersion = ref<string | null>(null)
  const loaded = ref(false)

  const role = computed(() => user.value?.role ?? null)
  const isAuthenticated = computed(() => user.value !== null)

  function apply(res: AuthResponse | null): void {
    user.value = res?.user ?? null
    mustAcceptPolicy.value = res?.mustAcceptPolicy ?? false
    policyVersion.value = res?.policyVersion ?? null
    setCsrfToken(res?.csrfToken ?? null)
    loaded.value = true
  }

  /** Загружает текущего пользователя один раз; без сессии — пользователь не задан. */
  async function ensureLoaded(): Promise<void> {
    if (loaded.value) return
    try {
      apply(await unwrap(api.GET('/auth/me')))
    } catch {
      apply(null)
    }
  }

  async function login(email: string, password: string): Promise<User> {
    const res = await unwrap(api.POST('/auth/login', { body: { email, password } }))
    apply(res)
    return res.user
  }

  async function logout(): Promise<void> {
    try {
      await unwrap(api.POST('/auth/logout'))
    } finally {
      apply(null)
    }
  }

  /** Сессия истекла (ответ 401): забыть пользователя. */
  function reset(): void {
    apply(null)
  }

  return {
    user,
    role,
    isAuthenticated,
    mustAcceptPolicy,
    policyVersion,
    loaded,
    ensureLoaded,
    login,
    logout,
    reset,
    setAuth: apply,
  }
})
