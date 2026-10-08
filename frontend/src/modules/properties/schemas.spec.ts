import { describe, expect, it } from 'vitest'
import { areaToApi, areaToText, propertySchema } from './schemas'

const valid = {
  code: 'Студия 5',
  kind: 'residential' as const,
  address: 'г. Руза, ул. Солнечная, д. 5, кв. 12',
  areaSqm: '24,5',
  floor: 3,
  rooms: 0,
  cadastralNumber: '50:19:0010101:123',
  ownershipBasis: '',
  furnished: true,
  description: '',
  expectedRentKop: 2800000,
  fiscalMode: 'none' as const,
  initialStatus: 'free' as const,
  statusSince: '2026-10-16',
}

describe('форма объекта', () => {
  it('принимает обязательные поля (ТК-01)', () => {
    expect(propertySchema.safeParse(valid).success).toBe(true)
  })

  it('проверяет площадь, кадастровый номер и комнаты нежилого', () => {
    const r = propertySchema.safeParse({
      ...valid,
      areaSqm: '24,555',
      cadastralNumber: '50-19',
      kind: 'non_residential',
      rooms: 1,
    })
    expect(r.success).toBe(false)
    const fields = r.error?.issues.map((i) => i.path[0])
    expect(fields).toEqual(expect.arrayContaining(['areaSqm', 'cadastralNumber', 'rooms']))
  })

  it('переводит площадь', () => {
    expect(areaToApi('24,5')).toBe('24.50')
    expect(areaToApi('48')).toBe('48.00')
    expect(areaToText('24.50')).toBe('24,5')
    expect(areaToText('48.00')).toBe('48')
  })
})
