import { z } from 'zod'
import { isValidInn } from '@/utils/inn'

const required = (msg: string) => z.string().trim().min(1, msg)
const optional = (re: RegExp, msg: string) =>
  z
    .string()
    .trim()
    .refine((v) => v === '' || re.test(v), msg)

/** Вкладка «Организация» (14 Экраны собственника, раздел 16.1). */
export const organizationSchema = z.object({
  fullName: required('Укажите полное наименование').max(300),
  shortName: required('Укажите краткое наименование').max(120),
  inn: z
    .string()
    .trim()
    .refine((v) => isValidInn(v), 'Неверный ИНН'),
  ogrnip: optional(/^\d{15}$/, 'ОГРНИП — 15 цифр'),
  address: required('Укажите адрес').max(300),
  documentCity: required('Укажите город для документов').max(120),
  phone: z.string().trim().max(40),
  email: z.string().trim().min(1, 'Укажите e-mail').pipe(z.email('Неверный формат e-mail')),
  signatoryName: required('Укажите подписанта').max(120),
  bankName: z.string().trim().max(200),
  bankBik: optional(/^\d{9}$/, 'БИК — 9 цифр'),
  bankAccount: optional(/^\d{20}$/, 'Расчётный счёт — 20 цифр'),
  bankCorrAccount: optional(/^\d{20}$/, 'Корреспондентский счёт — 20 цифр'),
})

export type OrganizationForm = z.infer<typeof organizationSchema>

/** Вкладка «ПДн» (раздел 16.8). */
export const pdnSchema = z.object({
  pdnRetentionYears: z.number({ error: 'Укажите срок' }).int().min(1, 'От 1 до 10 лет').max(10, 'От 1 до 10 лет'),
  pdnConsentPurpose: required('Укажите цель отдельного согласия').max(1000),
})

export const userCreateSchema = z.object({
  displayName: required('Укажите имя').max(120),
  email: z.string().trim().min(1, 'Укажите e-mail').pipe(z.email('Неверный формат e-mail')),
})

/** Следующая версия политики: 1.0 → 1.1, 1.9 → 1.10. */
export function nextPolicyVersion(version: string): string {
  const m = /^(\d+)\.(\d+)$/.exec(version.trim())
  if (!m) return '1.0'
  return `${m[1]}.${Number(m[2]) + 1}`
}
