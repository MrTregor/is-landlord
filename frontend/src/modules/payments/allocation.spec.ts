import { describe, expect, it } from 'vitest'
import type { Charge } from '@/api/queries/charges'
import { previewAllocation } from './allocation'

let seq = 0
function charge(
  id: string,
  kind: Charge['kind'],
  dueDate: string | null,
  outstandingKop: number,
  extra: Partial<Charge> = {},
) {
  seq++
  return {
    id,
    kind,
    dueDate,
    outstandingKop,
    status: 'open',
    penaltyState: kind === 'penalty' ? 'final' : null,
    createdAt: `2026-10-01T00:00:${String(seq).padStart(2, '0')}Z`,
    ...extra,
  } as Charge
}

describe('предпросмотр распределения (FR-07.1, 05 разд. 10.3)', () => {
  it('пример A1: 40 000 → сентябрь 28 000, октябрь 12 000 (ТК-21)', () => {
    const list = [
      charge('A', 'rent', '2026-09-10', 2800000),
      charge('B', 'utilities', '2026-10-10', 345060),
      charge('C', 'rent', '2026-10-10', 2800000),
      charge('P', 'penalty', '2026-10-12', 12000),
    ]
    const r = previewAllocation(4000000, list)
    expect(r.lines.map((l) => [l.charge.id, l.amountKop])).toEqual([
      ['A', 2800000],
      ['C', 1200000],
    ])
    expect(r.advanceKop).toBe(0)
  })
  it('выбранные начисления — первыми, остаток — аванс', () => {
    const list = [charge('A', 'rent', '2026-09-10', 1000), charge('P', 'penalty', '2026-10-12', 500)]
    const r = previewAllocation(2000, list, ['P'])
    expect(r.lines.map((l) => l.charge.id)).toEqual(['P', 'A'])
    expect(r.advanceKop).toBe(500)
  })
  it('начисляемые пени не погашаются', () => {
    const list = [charge('P', 'penalty', null, 500, { penaltyState: 'accruing' })]
    const r = previewAllocation(300, list, ['P'])
    expect(r.lines).toEqual([])
    expect(r.advanceKop).toBe(300)
  })
})
