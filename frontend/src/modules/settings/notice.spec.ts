import { describe, expect, it } from 'vitest'
import { parseNoticeDays } from './notice'

describe('дни уведомления об окончании договора (FR-10.3)', () => {
  it('разбирает список чисел', () => {
    expect(parseNoticeDays('90, 30')).toEqual([90, 30])
    expect(parseNoticeDays('30 90 30')).toEqual([90, 30])
    expect(parseNoticeDays('7;14')).toEqual([14, 7])
  })
  it('сообщает об ошибке', () => {
    expect(parseNoticeDays('')).toBe('Укажите хотя бы одно число дней')
    expect(parseNoticeDays('90, 0')).toBe('Число дней — от 1 до 365')
    expect(parseNoticeDays('девяносто')).toBe('«девяносто» — не целое число дней')
    expect(parseNoticeDays('400')).toBe('Число дней — от 1 до 365')
  })
})
