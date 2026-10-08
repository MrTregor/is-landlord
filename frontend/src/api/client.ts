// Клиент API: openapi-fetch с типами, сгенерированными из api/openapi.yaml (make gen).
import createClient, { type Middleware } from 'openapi-fetch'
import type { paths } from './schema'
import { ApiError, type Problem } from '@/utils/problem'

export const API_BASE = '/api/v1'

let csrfToken: string | null = null
let onUnauthorized: (() => void) | null = null

/** CSRF-токен сессии (из /auth/login или /auth/me). */
export function setCsrfToken(token: string | null): void {
  csrfToken = token
}

/** Текущий CSRF-токен (для запросов, собираемых вручную, например multipart). */
export function getCsrfToken(): string | null {
  return csrfToken
}

/** Что делать при 401 вне методов входа (например, перейти на страницу входа). */
export function setUnauthorizedHandler(handler: () => void): void {
  onUnauthorized = handler
}

const MUTATING = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

const middleware: Middleware = {
  onRequest({ request }) {
    if (MUTATING.has(request.method) && csrfToken) request.headers.set('X-CSRF-Token', csrfToken)
    return request
  },
  onResponse({ request, response }) {
    if (response.status === 401 && !new URL(request.url).pathname.startsWith(`${API_BASE}/auth/`)) {
      onUnauthorized?.()
    }
    return response
  },
}

export const api = createClient<paths>({ baseUrl: API_BASE, credentials: 'include' })
api.use(middleware)

/** Результат вызова openapi-fetch → данные или исключение ApiError с problem+json. */
export async function unwrap<T>(call: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const { data, error, response } = await call
  if (!response.ok) {
    const problem: Problem =
      error && typeof error === 'object' && 'code' in error
        ? (error as Problem)
        : { type: 'about:blank', title: response.statusText || 'Ошибка', status: response.status, code: 'unknown' }
    throw new ApiError(problem, response.status)
  }
  return data as T
}
