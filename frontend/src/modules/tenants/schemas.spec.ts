import { describe, expect, it } from 'vitest'
import { emptyTenantForm, tenantSchema } from './schemas'

const issues = (v: unknown) =>
  tenantSchema.safeParse(v).error?.issues.map((i) => `${String(i.path[0])}: ${i.message}`) ?? []

describe('форма нанимателя', () => {
  it('физлицу нужны фамилия, имя и телефон или e-mail', () => {
    expect(issues(emptyTenantForm())).toEqual(
      expect.arrayContaining([
        'lastName: Укажите фамилию',
        'firstName: Укажите имя',
        'phone: Укажите телефон или e-mail',
      ]),
    )
    const ok = { ...emptyTenantForm(), lastName: 'Кузнецов', firstName: 'Павел', phone: '+7 (916) 123-45-67' }
    expect(tenantSchema.safeParse(ok).success).toBe(true)
  })

  it('юрлицо с неверным ИНН — «Неверный ИНН» (ТК-05)', () => {
    const legal = {
      ...emptyTenantForm('legal'),
      orgName: 'ООО «Ромашка»',
      inn: '5075123458',
      legalAddress: 'г. Руза',
      signatoryName: 'Иванов И. И.',
      signatoryPosition: 'генеральный директор',
      signatoryBasis: 'Устава',
    }
    expect(issues(legal)).toContain('inn: Неверный ИНН')
    expect(tenantSchema.safeParse({ ...legal, inn: '5075123457' }).success).toBe(true)
  })

  it('проверяет формат паспорта и кода подразделения', () => {
    const v = {
      ...emptyTenantForm(),
      lastName: 'А',
      firstName: 'Б',
      email: 'a@b.ru',
      docNumber: '4518123456',
      docDivisionCode: '770001',
    }
    expect(issues(v)).toEqual(
      expect.arrayContaining([
        'docNumber: Серия и номер паспорта: 99 99 999999',
        'docDivisionCode: Код подразделения: 999-999',
      ]),
    )
  })
})
