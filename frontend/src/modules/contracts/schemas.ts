// Мастер договора (14 Экраны собственника, раздел 4.2) и проверки черновика (05 разд. 4.3).
import dayjs from 'dayjs'
import { z } from 'zod'
import type { ContractDetail, ContractInput } from '@/api/queries/contracts'
import type { components } from '@/api/schema'

type S = components['schemas']
const ISO = 'YYYY-MM-DD'

export interface OccupantRow {
  fullName: string
  birthDate: string | null
  relation: string
}

export interface ContractForm {
  propertyId: string | null
  tenantId: string | null
  startDate: string | null
  endDate: string | null
  rentKop: number | null
  paymentDay: number | null
  firstDueDate: string | null
  depositKop: number | null
  utilitiesMode: S['UtilitiesMode']
  utilitiesFixedKop: number | null
  penaltyEnabled: boolean
  penaltyRatePct: string
  penaltyGraceDays: number | null
  penaltyCapPct: string
  penaltyOnUtilities: boolean
  indexationEnabled: boolean
  indexationPct: string
  indexationPeriodMonths: number | null
  occupants: OccupantRow[]
  specialTerms: string
  notes: string
}

export function emptyContractForm(propertyId: string | null = null): ContractForm {
  return {
    propertyId,
    tenantId: null,
    startDate: null,
    endDate: null,
    rentKop: null,
    paymentDay: null,
    firstDueDate: null,
    depositKop: 0,
    utilitiesMode: 'included',
    utilitiesFixedKop: null,
    penaltyEnabled: false,
    penaltyRatePct: '0,1',
    penaltyGraceDays: 3,
    penaltyCapPct: '',
    penaltyOnUtilities: false,
    indexationEnabled: false,
    indexationPct: '5',
    indexationPeriodMonths: 12,
    occupants: [],
    specialTerms: '',
    notes: '',
  }
}

/** Вид договора (05 разд. 4.1): жилой объект и физлицо → найм (гл. 35), иначе аренда (гл. 34). */
export function legalBasis(propertyKind: S['PropertyKind'], tenantKind: S['TenantKind']): S['LegalBasis'] {
  return propertyKind === 'residential' && tenantKind === 'individual' ? 'ch35' : 'ch34'
}

export const legalBasisTitle: Record<S['LegalBasis'], string> = {
  ch35: 'Договор найма жилого помещения (гл. 35 ГК РФ)',
  ch34: 'Договор аренды (гл. 34 ГК РФ)',
}

/** Окончание = начало + срок − 1 день («11 месяцев», «1 год»). */
export function endAfter(start: string, months: number): string {
  return dayjs(start, ISO).add(months, 'month').subtract(1, 'day').format(ISO)
}

/** Срок от года: окончание ≥ начало + 1 год − 1 день (как столбец registration_required). */
export function registrationRequired(start: string, end: string): boolean {
  return !dayjs(end, ISO).isBefore(dayjs(endAfter(start, 12), ISO))
}

/** Краткосрочный наём — договор найма сроком менее года (ст. 683 ГК РФ). */
export function isShortTerm(basis: S['LegalBasis'], start: string, end: string): boolean {
  return basis === 'ch35' && !registrationRequired(start, end)
}

/** День оплаты по умолчанию: день даты начала, если ≤ 28, иначе 28. */
export function defaultPaymentDay(start: string): number {
  return Math.min(dayjs(start, ISO).date(), 28)
}

/** Первая индексация (05 разд. 8.1): начало + период; не первое число → первое число следующего месяца. */
export function firstIndexation(start: string, months: number): string {
  const x = dayjs(start, ISO).add(months, 'month')
  return (x.date() === 1 ? x : x.startOf('month').add(1, 'month')).format(ISO)
}

/** «0,1» → «0.1»; пусто → null. */
export function pctToApi(v: string): string | null {
  const s = v.trim().replace(',', '.')
  return s === '' ? null : s
}

/** «0.1000» → «0,1». */
export function pctToText(v: string | null | undefined): string {
  if (!v) return ''
  return v
    .replace(/(\.\d*?)0+$/, '$1')
    .replace(/\.$/, '')
    .replace('.', ',')
}

const pct = /^\d{1,3}([.,]\d{1,4})?$/
const num = (v: string) => Number(v.replace(',', '.'))

/** Шаг 1 «Стороны». */
export const partiesSchema = z.object({
  propertyId: z.string({ error: 'Выберите объект' }).min(1, 'Выберите объект'),
  tenantId: z.string({ error: 'Выберите нанимателя' }).min(1, 'Выберите нанимателя'),
})

/** Шаг 2 «Срок и плата» (05 разд. 4.3; ограничение пяти лет — для найма, передаётся отдельно). */
export function termsSchema(basis: S['LegalBasis'] | null) {
  return z
    .object({
      startDate: z.string({ error: 'Укажите дату начала' }).min(1, 'Укажите дату начала'),
      endDate: z.string({ error: 'Укажите дату окончания' }).min(1, 'Укажите дату окончания'),
      rentKop: z.number({ error: 'Укажите размер платы' }).int().positive('Укажите размер платы'),
      paymentDay: z
        .number({ error: 'День оплаты — от 1 до 28' })
        .int()
        .min(1, 'День оплаты — от 1 до 28')
        .max(28, 'День оплаты — от 1 до 28'),
      firstDueDate: z.string({ error: 'Укажите срок первой оплаты' }).min(1, 'Укажите срок первой оплаты'),
      depositKop: z.number().int().min(0, 'Сумма не может быть отрицательной').nullable(),
    })
    .superRefine((v, ctx) => {
      const start = dayjs(v.startDate, ISO)
      const end = dayjs(v.endDate, ISO)
      if (!end.isAfter(start)) {
        ctx.addIssue({ code: 'custom', path: ['endDate'], message: 'Дата окончания должна быть позже даты начала' })
      } else if (basis === 'ch35' && !end.isBefore(start.add(5, 'year'))) {
        ctx.addIssue({
          code: 'custom',
          path: ['endDate'],
          message: 'Договор найма заключается на срок не более пяти лет (ст. 683 ГК РФ)',
        })
      }
      const due = dayjs(v.firstDueDate, ISO)
      if (Math.abs(due.diff(start, 'day')) > 30) {
        ctx.addIssue({
          code: 'custom',
          path: ['firstDueDate'],
          message: 'Срок первой оплаты — не дальше 30 дней от даты начала',
        })
      }
    })
}

/** Шаг 3 «ЖКУ, пени, индексация». */
export const conditionsSchema = z
  .object({
    utilitiesMode: z.enum(['included', 'meters', 'fixed']),
    utilitiesFixedKop: z.number().int().min(0, 'Сумма не может быть отрицательной').nullable(),
    penaltyEnabled: z.boolean(),
    penaltyRatePct: z.string(),
    penaltyGraceDays: z
      .number({ error: 'Льготных дней — от 0 до 30' })
      .int()
      .min(0)
      .max(30, 'Льготных дней — от 0 до 30'),
    penaltyCapPct: z.string(),
    indexationEnabled: z.boolean(),
    indexationPct: z.string(),
    indexationPeriodMonths: z
      .number({ error: 'Индексация — не чаще одного раза в 12 месяцев' })
      .int()
      .min(12, 'Индексация — не чаще одного раза в 12 месяцев'),
  })
  .superRefine((v, ctx) => {
    if (v.utilitiesMode === 'fixed' && !(v.utilitiesFixedKop && v.utilitiesFixedKop > 0)) {
      ctx.addIssue({ code: 'custom', path: ['utilitiesFixedKop'], message: 'Укажите фиксированную сумму ЖКУ' })
    }
    if (v.penaltyEnabled) {
      const r = v.penaltyRatePct.trim()
      if (!pct.test(r) || num(r) < 0.01 || num(r) > 1) {
        ctx.addIssue({ code: 'custom', path: ['penaltyRatePct'], message: 'Ставка пеней — от 0,01 до 1 % в день' })
      }
      const c = v.penaltyCapPct.trim()
      if (c !== '' && (!pct.test(c) || num(c) <= 0 || num(c) > 100)) {
        ctx.addIssue({ code: 'custom', path: ['penaltyCapPct'], message: 'Предел пеней — от 0,01 до 100 % от суммы' })
      }
    }
    if (v.indexationEnabled) {
      const p = v.indexationPct.trim()
      if (!pct.test(p) || num(p) < 0.01 || num(p) > 30) {
        ctx.addIssue({ code: 'custom', path: ['indexationPct'], message: 'Процент индексации — от 0,01 до 30' })
      }
    }
  })

/** Шаг 4 «Проживающие и особые условия». */
export const occupantsSchema = z.object({
  occupants: z.array(
    z.object({
      fullName: z.string().trim().min(1, 'Укажите ФИО проживающего').max(200),
      birthDate: z.string().nullable(),
      relation: z.string().max(100),
    }),
  ),
  specialTerms: z.string().max(10000),
  notes: z.string().max(10000),
})

/** Ошибки схемы по полям: «occupants.0.fullName» → текст. */
export function fieldErrors(schema: z.ZodType, value: unknown): Record<string, string> {
  const r = schema.safeParse(value)
  const out: Record<string, string> = {}
  if (r.success) return out
  for (const i of r.error.issues) {
    const key = i.path.map(String).join('.')
    if (!(key in out)) out[key] = i.message
  }
  return out
}

/** Предупреждения мастера (05 разд. 4.3) — не блокируют сохранение. */
export function wizardWarnings(
  f: ContractForm,
  propertyKind: S['PropertyKind'] | null,
  fiscalMode: S['FiscalMode'] | null,
): string[] {
  const w: string[] = []
  if (
    f.startDate &&
    f.endDate &&
    dayjs(f.endDate).isAfter(dayjs(f.startDate)) &&
    registrationRequired(f.startDate, f.endDate)
  ) {
    w.push(
      'Договор на срок от года подлежит государственной регистрации (п. 2 ст. 674 ГК РФ / ст. 651 ГК РФ). ' +
        'Напоминание будет добавлено в календарь',
    )
  }
  if (propertyKind === 'non_residential' && fiscalMode === 'none') {
    w.push('По нежилым объектам освобождение от применения ККТ не действует')
  }
  return w
}

/** Форма → тело запроса ContractInput. */
export function toInput(f: ContractForm): ContractInput {
  return {
    propertyId: f.propertyId!,
    tenantId: f.tenantId!,
    startDate: f.startDate!,
    endDate: f.endDate!,
    firstDueDate: f.firstDueDate,
    rentKop: f.rentKop ?? 0,
    paymentDay: f.paymentDay ?? 1,
    depositKop: f.depositKop ?? 0,
    utilitiesMode: f.utilitiesMode,
    utilitiesFixedKop: f.utilitiesMode === 'included' ? 0 : (f.utilitiesFixedKop ?? 0),
    penaltyEnabled: f.penaltyEnabled,
    penaltyRatePct: f.penaltyEnabled ? pctToApi(f.penaltyRatePct) : null,
    penaltyGraceDays: f.penaltyGraceDays ?? 0,
    penaltyCapPct: f.penaltyEnabled ? pctToApi(f.penaltyCapPct) : null,
    penaltyOnUtilities: f.penaltyEnabled && f.penaltyOnUtilities,
    indexationEnabled: f.indexationEnabled,
    indexationPct: f.indexationEnabled ? pctToApi(f.indexationPct) : null,
    indexationPeriodMonths: f.indexationPeriodMonths ?? 12,
    specialTerms: f.specialTerms.trim() || null,
    notes: f.notes.trim() || null,
    occupants: f.occupants.map((o) => ({
      fullName: o.fullName.trim(),
      birthDate: o.birthDate,
      relation: o.relation.trim() || null,
    })),
  }
}

/** Черновик из API → форма мастера. */
export function fromDetail(d: ContractDetail): ContractForm {
  return {
    propertyId: d.property.id,
    tenantId: d.tenant.id,
    startDate: d.startDate,
    endDate: d.endDate,
    rentKop: d.currentTerms.rentKop,
    paymentDay: d.currentTerms.paymentDay,
    firstDueDate: d.firstDueDate,
    depositKop: d.depositKop,
    utilitiesMode: d.utilitiesMode,
    utilitiesFixedKop: d.currentTerms.utilitiesFixedKop || null,
    penaltyEnabled: d.penaltyEnabled,
    penaltyRatePct: pctToText(d.penaltyRatePct) || '0,1',
    penaltyGraceDays: d.penaltyGraceDays,
    penaltyCapPct: pctToText(d.penaltyCapPct),
    penaltyOnUtilities: d.penaltyOnUtilities,
    indexationEnabled: d.indexationEnabled,
    indexationPct: pctToText(d.indexationPct) || '5',
    indexationPeriodMonths: d.indexationPeriodMonths,
    occupants: d.occupants.map((o) => ({ fullName: o.fullName, birthDate: o.birthDate, relation: o.relation ?? '' })),
    specialTerms: d.specialTerms ?? '',
    notes: d.notes ?? '',
  }
}

/** Плата за неполный последний месяц при расторжении (05 разд. 5.2) — для подсказки в диалоге «Расторгнуть». */
export function lastMonthRent(
  rentKop: number,
  lastDay: string,
): { amountKop: number; days: number; daysInMonth: number } {
  const d = dayjs(lastDay, ISO)
  const daysInMonth = d.daysInMonth()
  const days = d.date()
  if (days === daysInMonth) return { amountKop: rentKop, days, daysInMonth }
  // rent × days / daysInMonth с округлением половины вверх — в целых копейках, без дробей.
  const total = rentKop * days
  const q = Math.floor(total / daysInMonth)
  const rem = total - q * daysInMonth
  return { amountKop: rem * 2 >= daysInMonth ? q + 1 : q, days, daysInMonth }
}
