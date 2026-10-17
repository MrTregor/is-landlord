import { describe, expect, it } from 'vitest'
import { presetPeriod } from './period'

describe('быстрые периоды отчётов (FR-11.1)', () => {
  const today = '2026-10-16'
  it('месяц, прошлый месяц', () => {
    expect(presetPeriod('month', today)).toEqual({ from: '2026-10-01', to: '2026-10-31' })
    expect(presetPeriod('prevMonth', today)).toEqual({ from: '2026-09-01', to: '2026-09-30' })
    expect(presetPeriod('prevMonth', '2026-01-10')).toEqual({ from: '2025-12-01', to: '2025-12-31' })
  })
  it('квартал и годы', () => {
    expect(presetPeriod('quarter', today)).toEqual({ from: '2026-10-01', to: '2026-12-31' })
    expect(presetPeriod('quarter', '2026-05-31')).toEqual({ from: '2026-04-01', to: '2026-06-30' })
    expect(presetPeriod('year', today)).toEqual({ from: '2026-01-01', to: '2026-12-31' })
    expect(presetPeriod('prevYear', today)).toEqual({ from: '2025-01-01', to: '2025-12-31' })
  })
})
