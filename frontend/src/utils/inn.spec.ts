import { describe, expect, it } from 'vitest'
import { isValidInn } from './inn'

// 16 Тестирование, раздел 2 «ИНН»: контрольные суммы 10 и 12 знаков.
describe('isValidInn', () => {
  it('принимает верные ИНН', () => {
    expect(isValidInn('507500363778')).toBe(true) // ИП Гордейчик А.В.
    expect(isValidInn('7704211201')).toBe(true) // ОФД из примера отчёта АТОЛ
  })

  it('отклоняет неверные', () => {
    expect(isValidInn('507500363779')).toBe(false)
    expect(isValidInn('7704211202')).toBe(false)
    expect(isValidInn('12345')).toBe(false)
    expect(isValidInn('77042112011')).toBe(false)
  })
})
