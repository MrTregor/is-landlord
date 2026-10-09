import { describe, expect, it } from 'vitest'
import {
  conditionsSchema,
  emptyContractForm,
  endAfter,
  fieldErrors,
  firstIndexation,
  isShortTerm,
  lastMonthRent,
  legalBasis,
  pctToApi,
  pctToText,
  registrationRequired,
  termsSchema,
  toInput,
  wizardWarnings,
} from './schemas'

describe('мастер договора (FR-03.1)', () => {
  it('вид договора по объекту и нанимателю (05 разд. 4.1)', () => {
    expect(legalBasis('residential', 'individual')).toBe('ch35')
    expect(legalBasis('residential', 'legal')).toBe('ch34')
    expect(legalBasis('non_residential', 'individual')).toBe('ch34')
  })

  it('быстрые сроки, регистрация и краткосрочный наём (FR-03.3, ТК-34)', () => {
    expect(endAfter('2026-03-15', 12)).toBe('2027-03-14')
    expect(endAfter('2026-10-01', 11)).toBe('2027-08-31')
    expect(registrationRequired('2026-03-15', '2027-03-14')).toBe(true)
    expect(registrationRequired('2026-10-01', '2027-08-31')).toBe(false)
    expect(isShortTerm('ch35', '2026-10-01', '2027-08-31')).toBe(true)
    expect(isShortTerm('ch34', '2026-10-01', '2027-08-31')).toBe(false)
  })

  it('первая индексация (05 разд. 8.1, FR-05.4)', () => {
    expect(firstIndexation('2026-03-15', 12)).toBe('2027-04-01')
    expect(firstIndexation('2026-03-01', 12)).toBe('2027-03-01')
  })

  it('проценты — строки без float в модели', () => {
    expect(pctToApi('0,1')).toBe('0.1')
    expect(pctToApi(' ')).toBeNull()
    expect(pctToText('0.1000')).toBe('0,1')
    expect(pctToText('5.00')).toBe('5')
  })

  it('проверки срока и платы (05 разд. 4.3)', () => {
    const base = {
      startDate: '2026-10-01',
      endDate: '2026-09-30',
      rentKop: 0,
      paymentDay: 29,
      firstDueDate: '2026-11-15',
      depositKop: 0,
    }
    const e = fieldErrors(termsSchema('ch35'), base)
    expect(e.endDate).toBe('Дата окончания должна быть позже даты начала')
    expect(e.rentKop).toBe('Укажите размер платы')
    expect(e.paymentDay).toBe('День оплаты — от 1 до 28')
    const five = fieldErrors(termsSchema('ch35'), {
      ...base,
      endDate: '2031-10-01',
      rentKop: 1,
      paymentDay: 1,
      firstDueDate: '2026-10-01',
    })
    expect(five.endDate).toContain('не более пяти лет')
    expect(
      fieldErrors(termsSchema('ch34'), {
        ...base,
        endDate: '2031-10-01',
        rentKop: 1,
        paymentDay: 1,
        firstDueDate: '2026-10-31',
      }),
    ).toEqual({})
  })

  it('пени, индексация и фиксированная часть ЖКУ', () => {
    const f = {
      ...emptyContractForm(),
      penaltyEnabled: true,
      penaltyRatePct: '1,5',
      indexationEnabled: true,
      indexationPct: '31',
      utilitiesMode: 'fixed' as const,
    }
    const e = fieldErrors(conditionsSchema, f)
    expect(e.penaltyRatePct).toBe('Ставка пеней — от 0,01 до 1 % в день')
    expect(e.indexationPct).toBe('Процент индексации — от 0,01 до 30')
    expect(e.utilitiesFixedKop).toBe('Укажите фиксированную сумму ЖКУ')
  })

  it('предупреждения и тело запроса', () => {
    const f = {
      ...emptyContractForm('p'),
      tenantId: 't',
      startDate: '2026-03-15',
      endDate: '2027-03-14',
      rentKop: 3000000,
      paymentDay: 15,
    }
    expect(wizardWarnings(f, 'non_residential', 'none')).toHaveLength(2)
    const body = toInput({ ...f, penaltyEnabled: false, penaltyRatePct: '0,1' })
    expect(body.penaltyRatePct).toBeNull()
    expect(body.utilitiesFixedKop).toBe(0)
  })

  it('плата за последний неполный месяц — R4 (05 разд. 5)', () => {
    expect(lastMonthRent(2800000, '2026-11-10').amountKop).toBe(933333)
    expect(lastMonthRent(3000000, '2026-03-31').amountKop).toBe(3000000)
    expect(lastMonthRent(3000000, '2027-03-14').amountKop).toBe(1354839)
  })
})
