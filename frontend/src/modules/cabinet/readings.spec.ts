import { describe, expect, it } from 'vitest'
import { consumption, formatMilli, formatReading, isHigh, parseReading, recentConsumptions } from './readings'

describe('показания в кабинете (FR-05.5)', () => {
  it('разбирает ввод: запятая, пробелы, до 3 знаков после запятой', () => {
    expect(parseReading('115,9')).toBe('115.9')
    expect(parseReading(' 115.900 ')).toBe('115.9')
    expect(parseReading('007')).toBe('7')
    expect(parseReading('1,2345')).toBeNull()
    expect(parseReading('1234567')).toBeNull()
    expect(parseReading('-1')).toBeNull()
    expect(parseReading('')).toBeNull()
  })

  it('показывает значение с тремя знаками и считает расход «3,500»', () => {
    expect(formatReading('112.4')).toBe('112,400')
    expect(formatMilli(consumption('112.4', '115.9')!)).toBe('3,500')
    expect(consumption(null, '1')).toBeNull()
    expect(formatMilli(-500)).toBe('-0,500')
  })

  it('предупреждает, если расход больше трёх средних', () => {
    expect(isHigh(12100, [3000, 4000, 5000])).toBe(true)
    expect(isHigh(12000, [3000, 4000, 5000])).toBe(false)
    expect(isHigh(100000, [])).toBe(false)
  })

  it('берёт расходы последних месяцев по принятым показаниям', () => {
    const h = [
      { value: '115.9', status: 'accepted' },
      { value: '120', status: 'rejected' },
      { value: '112.4', status: 'accepted' },
      { value: '110', status: 'accepted' },
    ]
    expect(recentConsumptions(h)).toEqual([3500, 2400])
  })
})
