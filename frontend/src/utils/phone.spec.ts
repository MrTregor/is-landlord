import { describe, expect, it } from 'vitest'
import { formatPhone, normalizePhone } from './phone'

describe('телефон', () => {
  it('нормализует в +7XXXXXXXXXX', () => {
    expect(normalizePhone('8 (916) 123-45-67')).toBe('+79161234567')
    expect(normalizePhone('+7 916 1234567')).toBe('+79161234567')
    expect(normalizePhone('9161234567')).toBe('+79161234567')
    expect(normalizePhone('12345')).toBeNull()
  })

  it('выводит в формате +7 (916) 123-45-67', () => {
    expect(formatPhone('+79161234567')).toBe('+7 (916) 123-45-67')
    expect(formatPhone(null)).toBe('')
  })
})
