import { describe, expect, it } from 'vitest'
import { formatKop, formatKopNumber, kopToRubString, parseRubToKop } from './money'

// Эталоны 16 Тестирование, раздел 2 «Деньги».
describe('parseRubToKop', () => {
  it('разбирает суммы с разрядами и копейками', () => {
    expect(parseRubToKop('28 000,50')).toBe(2800050)
    expect(parseRubToKop('0,1')).toBe(10)
    expect(parseRubToKop('26 500,00')).toBe(2650000)
    expect(parseRubToKop('15032.5')).toBe(1503250)
    expect(parseRubToKop('100')).toBe(10000)
  })

  it('отклоняет неоднозначный и неверный ввод', () => {
    expect(() => parseRubToKop('1.234,5')).toThrow()
    expect(() => parseRubToKop('12,345')).toThrow()
    expect(() => parseRubToKop('')).toThrow()
    expect(() => parseRubToKop('abc')).toThrow()
    expect(() => parseRubToKop('99999999999999999')).toThrow()
  })
})

describe('formatKop', () => {
  it('выводит рубли с неразрывными пробелами и знаком ₽', () => {
    expect(formatKop(2800000)).toBe('28 000,00 ₽')
    expect(formatKop(5)).toBe('0,05 ₽')
    expect(formatKopNumber(150325000)).toBe('1 503 250,00')
    expect(formatKopNumber(-100000)).toBe('−1 000,00')
  })

  it('готовит строку для поля ввода', () => {
    expect(kopToRubString(2800050)).toBe('28000,50')
    expect(parseRubToKop(kopToRubString(1645161))).toBe(1645161)
  })
})
