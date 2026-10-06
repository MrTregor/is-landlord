import { describe, expect, it } from 'vitest'
import { ApiError, parseProblem } from './problem'

describe('parseProblem', () => {
  it('раскладывает ошибки полей и detail', () => {
    const p = parseProblem({
      type: 'https://landlord.local/errors/validation_failed',
      title: 'Проверьте заполнение полей',
      status: 422,
      code: 'validation_failed',
      detail: 'Дата окончания должна быть позже даты начала',
      errors: [{ field: 'endDate', message: 'Дата окончания должна быть позже даты начала' }],
      requestId: 'r1',
    })
    expect(p.status).toBe(422)
    expect(p.message).toBe('Дата окончания должна быть позже даты начала')
    expect(p.fieldErrors).toEqual({ endDate: 'Дата окончания должна быть позже даты начала' })
  })

  it('берёт title, если detail пуст, и понимает ApiError', () => {
    const err = new ApiError({ type: 't', title: 'Доступ запрещён', status: 403, code: 'forbidden' }, 403)
    expect(parseProblem(err).message).toBe('Доступ запрещён')
  })

  it('сетевую ошибку описывает по-русски', () => {
    expect(parseProblem(new TypeError('Failed to fetch')).message).toMatch(/Нет связи с сервером/)
  })
})
