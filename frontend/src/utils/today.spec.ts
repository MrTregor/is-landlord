import { afterEach, describe, expect, it } from 'vitest'
import { toIsoDate } from './date'
import { isShifted, setSystemToday, today, todayIso } from './today'

describe('«сегодня» — дата системы (режим demo)', () => {
  afterEach(() => setSystemToday(null))

  it('без сдвига — дата компьютера', () => {
    expect(todayIso()).toBe(toIsoDate(new Date()))
    expect(isShifted()).toBe(false)
  })

  it('со сдвигом — дата системы, в том числе для календаря', () => {
    setSystemToday('2027-03-06')
    expect(todayIso()).toBe('2027-03-06')
    expect(toIsoDate(today())).toBe('2027-03-06')
    expect(isShifted()).toBe(true)
  })
})
