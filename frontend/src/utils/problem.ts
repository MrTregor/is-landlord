// Разбор ответа application/problem+json (06 API, раздел «Ошибки»).
import type { components } from '@/api/schema'

export type Problem = components['schemas']['Problem']

export interface ParsedProblem {
  status: number
  code: string
  /** Текст для пользователя: detail, иначе title. */
  message: string
  /** Ошибки полей: имя поля → текст. */
  fieldErrors: Record<string, string>
  requestId?: string
}

const NETWORK_MESSAGE = 'Нет связи с сервером. Проверьте подключение и повторите'

function isProblem(v: unknown): v is Problem {
  return typeof v === 'object' && v !== null && 'code' in v && 'status' in v
}

/** Превращает ошибку запроса (problem+json, сетевая ошибка, иное) в понятный пользователю вид. */
export function parseProblem(err: unknown): ParsedProblem {
  const body = err instanceof ApiError ? err.problem : err
  if (isProblem(body)) {
    const fieldErrors: Record<string, string> = {}
    for (const e of body.errors ?? []) {
      if (!(e.field in fieldErrors)) fieldErrors[e.field] = e.message
    }
    return {
      status: body.status,
      code: body.code,
      message: body.detail || body.title,
      fieldErrors,
      requestId: body.requestId,
    }
  }
  return { status: 0, code: 'network', message: NETWORK_MESSAGE, fieldErrors: {} }
}

/** Ошибка API с телом problem+json. */
export class ApiError extends Error {
  constructor(
    public readonly problem: Problem,
    public readonly status: number,
  ) {
    super(problem.detail || problem.title)
    this.name = 'ApiError'
  }
}
