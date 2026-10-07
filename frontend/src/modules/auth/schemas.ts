import { z } from 'zod'

/** Требования к паролю (12 Безопасность и ПДн, раздел 1). */
export const PASSWORD_RULE = 'Пароль должен быть не короче 10 символов и содержать буквы и цифры'

export const passwordRule = z
  .string()
  .min(10, PASSWORD_RULE)
  .refine((v) => /\p{L}/u.test(v) && /\d/.test(v), PASSWORD_RULE)

const email = z.string().trim().min(1, 'Введите e-mail').pipe(z.email('Неверный формат e-mail'))

export const loginSchema = z.object({
  email,
  password: z.string().min(1, 'Введите пароль'),
})

export const forgotSchema = z.object({ email })

export const newPasswordSchema = z
  .object({ password: passwordRule, repeat: z.string() })
  .refine((v) => v.password === v.repeat, { path: ['repeat'], message: 'Пароли не совпадают' })

export type LoginForm = z.infer<typeof loginSchema>
