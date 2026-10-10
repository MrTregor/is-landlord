import { describe, expect, it } from 'vitest'
import { penaltySegments, rentFormula, utilityLines } from './calcDetails'

const sp = (s: string | null) => s?.replace(/ /g, ' ')

describe('подробности расчёта (FR-05)', () => {
  it('плата за неполный месяц — R1', () => {
    expect(sp(rentFormula({ rent: 3000000, days: 17, daysInMonth: 31 }))).toBe('30 000,00 × 17 / 31 дн.')
    expect(sp(rentFormula({ rent: 3000000, days: 30, daysInMonth: 30 }))).toBe('полный месяц: 30 000,00')
    expect(rentFormula({})).toBeNull()
  })
  it('пени по отрезкам — P1', () => {
    const segs = penaltySegments({
      ratePct: '0.1000',
      segments: [{ from: '2026-10-09', to: '2026-10-16', base: 1150000, days: 8, amount: 9200 }],
    })
    expect(segs.map(sp)).toEqual(['09.10–16.10: 8 дн. × 11 500,00 × 0,1 % = 92,00'])
  })
  it('строки ЖКУ — U1', () => {
    const [l] = utilityLines({
      lines: [
        {
          type: 'cold_water',
          prev: '112.400',
          prevDate: '2026-08-31',
          value: '115.900',
          consumption: '3.500',
          tariff: '54.3700',
          amount: 19030,
        },
      ],
    })
    expect({ ...l, amount: sp(l!.amount) }).toMatchObject({
      type: 'Холодная вода',
      prev: '112,4',
      consumption: '3,5',
      tariff: '54,37',
      amount: '190,30 ₽',
    })
  })
})
