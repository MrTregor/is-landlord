import { z } from 'zod'

export const loginSchema = z.object({
  email: z.string().trim().min(1, 'Введите e-mail').pipe(z.email('Неверный формат e-mail')),
  password: z.string().min(1, 'Введите пароль'),
})

export type LoginForm = z.infer<typeof loginSchema>
