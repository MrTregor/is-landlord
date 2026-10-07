import { describe, expect, it } from 'vitest'
import { nextPolicyVersion, organizationSchema } from './schemas'

describe('настройки', () => {
  it('следующая версия политики', () => {
    expect(nextPolicyVersion('1.0')).toBe('1.1')
    expect(nextPolicyVersion('1.9')).toBe('1.10')
    expect(nextPolicyVersion('нет')).toBe('1.0')
  })

  it('проверяет реквизиты организации', () => {
    const ok = organizationSchema.safeParse({
      fullName: 'Индивидуальный предприниматель Гордейчик Александр Васильевич',
      shortName: 'ИП Гордейчик А.В.',
      inn: '507500363778',
      ogrnip: '317502400029263',
      address: 'Московская область, г. Руза',
      documentCity: 'г. Руза',
      phone: '',
      email: 'owner@landlord.local',
      signatoryName: 'А.В. Гордейчик',
      bankName: '',
      bankBik: '044525999',
      bankAccount: '40802810900000012345',
      bankCorrAccount: '',
    })
    expect(ok.success).toBe(true)

    const bad = organizationSchema.safeParse({ ...(ok.success ? ok.data : {}), inn: '507500363779', bankBik: '12' })
    expect(bad.success).toBe(false)
    const fields = bad.error?.issues.map((i) => i.path[0])
    expect(fields).toContain('inn')
    expect(fields).toContain('bankBik')
  })
})
