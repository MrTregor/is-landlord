import { z } from 'zod'
import { isValidInn } from '@/utils/inn'
import { normalizePhone } from '@/utils/phone'

const s = (max: number) => z.string().trim().max(max, `Не более ${max} символов`)

/** Форма нанимателя (14 Экраны собственника, раздел 3.2). */
export const tenantSchema = z
  .object({
    kind: z.enum(['individual', 'legal']),
    lastName: s(100),
    firstName: s(100),
    middleName: s(100),
    birthDate: z.string().nullable(),
    docType: z.enum(['passport_rf', 'foreign_passport', 'other']).nullable(),
    docNumber: s(40),
    docIssuedBy: s(300),
    docIssuedOn: z.string().nullable(),
    docDivisionCode: s(20),
    registrationAddress: s(500),
    orgName: s(300),
    inn: s(12),
    kpp: s(9),
    ogrn: s(15),
    legalAddress: s(500),
    signatoryName: s(200),
    signatoryPosition: s(200),
    signatoryBasis: s(300),
    phone: s(40),
    email: s(254),
    note: s(4000),
  })
  .superRefine((v, ctx) => {
    const req = (field: keyof typeof v, message: string) => {
      if (!String(v[field] ?? '').trim()) ctx.addIssue({ code: 'custom', path: [field], message })
    }
    if (v.kind === 'individual') {
      req('lastName', 'Укажите фамилию')
      req('firstName', 'Укажите имя')
      if (v.inn && !(v.inn.length === 12 && isValidInn(v.inn))) {
        ctx.addIssue({ code: 'custom', path: ['inn'], message: 'Неверный ИНН' })
      }
      if (!v.phone.trim() && !v.email.trim()) {
        ctx.addIssue({ code: 'custom', path: ['phone'], message: 'Укажите телефон или e-mail' })
      }
      if (v.docType === 'passport_rf' && v.docNumber && !/^\d{2} \d{2} \d{6}$/.test(v.docNumber)) {
        ctx.addIssue({ code: 'custom', path: ['docNumber'], message: 'Серия и номер паспорта: 99 99 999999' })
      }
      if (v.docDivisionCode && !/^\d{3}-\d{3}$/.test(v.docDivisionCode)) {
        ctx.addIssue({ code: 'custom', path: ['docDivisionCode'], message: 'Код подразделения: 999-999' })
      }
    } else {
      req('orgName', 'Укажите наименование')
      req('legalAddress', 'Укажите юридический адрес')
      req('signatoryName', 'Укажите подписанта')
      req('signatoryPosition', 'Укажите должность подписанта')
      req('signatoryBasis', 'Укажите основание полномочий')
      if (!(v.inn.length === 10 && isValidInn(v.inn))) {
        ctx.addIssue({ code: 'custom', path: ['inn'], message: v.inn ? 'Неверный ИНН' : 'Укажите ИНН' })
      }
      if (v.kpp && !/^\d{9}$/.test(v.kpp)) ctx.addIssue({ code: 'custom', path: ['kpp'], message: 'КПП — 9 цифр' })
      if (v.ogrn && !/^\d{13}(\d{2})?$/.test(v.ogrn)) {
        ctx.addIssue({ code: 'custom', path: ['ogrn'], message: 'ОГРН — 13 цифр (ОГРНИП — 15)' })
      }
    }
    if (v.phone.trim() && !normalizePhone(v.phone)) {
      ctx.addIssue({ code: 'custom', path: ['phone'], message: 'Телефон — 10 цифр после +7' })
    }
    if (v.email.trim() && !z.email().safeParse(v.email.trim()).success) {
      ctx.addIssue({ code: 'custom', path: ['email'], message: 'Неверный формат e-mail' })
    }
  })

export type TenantForm = z.infer<typeof tenantSchema>

export const emptyTenantForm = (kind: TenantForm['kind'] = 'individual'): TenantForm => ({
  kind,
  lastName: '',
  firstName: '',
  middleName: '',
  birthDate: null,
  docType: 'passport_rf',
  docNumber: '',
  docIssuedBy: '',
  docIssuedOn: null,
  docDivisionCode: '',
  registrationAddress: '',
  orgName: '',
  inn: '',
  kpp: '',
  ogrn: '',
  legalAddress: '',
  signatoryName: '',
  signatoryPosition: '',
  signatoryBasis: '',
  phone: '',
  email: '',
  note: '',
})
