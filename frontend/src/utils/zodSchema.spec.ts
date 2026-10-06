import { describe, expect, it } from 'vitest'
import { z } from 'zod'
import { toTypedSchema } from './zodSchema'

describe('toTypedSchema', () => {
  const schema = toTypedSchema(
    z.object({
      email: z.email('Введите e-mail'),
      password: z.string().min(1, 'Введите пароль'),
    }),
  )

  it('возвращает значение при успехе', async () => {
    const r = await schema.parse({ email: 'owner@landlord.local', password: 'x' })
    expect(r.errors).toEqual([])
    expect(r.value).toEqual({ email: 'owner@landlord.local', password: 'x' })
  })

  it('возвращает ошибки по полям', async () => {
    const r = await schema.parse({ email: 'нет', password: '' })
    expect(r.errors).toEqual([
      { path: 'email', errors: ['Введите e-mail'] },
      { path: 'password', errors: ['Введите пароль'] },
    ])
  })
})
