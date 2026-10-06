import { describe, expect, it } from 'vitest'
import {
  formatDate,
  formatDateLong,
  formatDateTime,
  formatMonth,
  formatPeriod,
  formatTime,
  fromIsoDate,
  toIsoDate,
} from './date'

describe('даты', () => {
  it('переводит Date ↔ строку без сдвига часового пояса', () => {
    expect(toIsoDate(new Date(2026, 9, 16))).toBe('2026-10-16')
    expect(toIsoDate(fromIsoDate('2026-03-01'))).toBe('2026-03-01')
    expect(() => fromIsoDate('2026-02-30')).toThrow()
  })

  it('форматирует по правилам 13 Фронтенд, раздел 8', () => {
    expect(formatDate('2026-10-16')).toBe('16.10.2026')
    expect(formatDate(null)).toBe('')
    expect(formatDateLong('2026-10-16')).toBe('16 октября 2026')
    expect(formatMonth('2026-10-01')).toBe('октябрь 2026')
    expect(formatPeriod('2026-03-15', '2026-03-31')).toBe('15–31 марта 2026')
    expect(formatPeriod('2026-10-01', '2026-11-30')).toBe('01.10.2026 – 30.11.2026')
  })

  it('выводит время по Москве', () => {
    expect(formatTime('2026-10-16T16:29:00Z')).toBe('19:29')
    expect(formatDateTime('2026-10-16T16:29:00Z')).toBe('16.10.2026 19:29')
  })
})
