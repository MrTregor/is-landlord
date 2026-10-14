import { describe, expect, it } from 'vitest'
import type { CabinetSummary } from '@/api/queries/cabinet'
import { retryPlan } from './retry'

const summary = (lines: string[], depositToPayKop = 0) =>
  ({ lines: lines.map((chargeId) => ({ chargeId })), depositToPayKop }) as unknown as CabinetSummary

describe('повтор оплаты (FR-13, open-questions № 54)', () => {
  it('оплачиваются только начисления, открытые сейчас', () => {
    expect(retryPlan({ purpose: 'charges', chargeIds: ['a', 'b'] }, summary(['b', 'c']))).toEqual({ chargeIds: ['b'] })
  })
  it('открытых не осталось — «Начисления уже оплачены»', () => {
    expect(retryPlan({ purpose: 'charges', chargeIds: ['a'] }, summary(['c']))).toEqual({
      message: 'Начисления уже оплачены',
    })
  })
  it('залог: внесён — сообщение, иначе повтор', () => {
    expect(retryPlan({ purpose: 'deposit', chargeIds: [] }, summary([], 0))).toEqual({
      message: 'Обеспечительный платёж уже внесён',
    })
    expect(retryPlan({ purpose: 'deposit', chargeIds: [] }, summary([], 100))).toEqual({ chargeIds: [] })
  })
})
