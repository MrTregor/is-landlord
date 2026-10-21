import { describe, expect, it } from 'vitest'
import { nextTick, reactive } from 'vue'
import { useSaveState } from './saveState'

describe('useSaveState (FR-14.1)', () => {
  it('после сохранения — «Сохранено», при изменении формы — снова «Сохранить»', async () => {
    const form = reactive({ inn: '507500363778' })
    const s = useSaveState(() => form)
    expect(s.button.value).toMatchObject({ label: 'Сохранить', icon: undefined })

    s.saved()
    expect(s.button.value).toMatchObject({ label: 'Сохранено', icon: 'pi pi-check', severity: 'success' })

    // Форма заново заполнена теми же значениями (ответ сервера после сохранения).
    form.inn = '507500363778'
    await nextTick()
    expect(s.state.value).toBe('saved')

    form.inn = '507500363779'
    await nextTick()
    expect(s.button.value.label).toBe('Сохранить')
  })

  it('после ошибки — «Ошибка, попробуйте ещё раз» до изменения или новой попытки', async () => {
    const form = reactive({ email: 'owner@landlord.local' })
    const s = useSaveState(() => form)
    s.failed()
    expect(s.button.value).toMatchObject({
      label: 'Ошибка, попробуйте ещё раз',
      icon: 'pi pi-times',
      severity: 'danger',
    })

    s.reset()
    expect(s.button.value.label).toBe('Сохранить')

    s.failed()
    form.email = 'owner@example.ru'
    await nextTick()
    expect(s.state.value).toBe('idle')
  })

  it('своя подпись обычного состояния', () => {
    const s = useSaveState(() => [], 'Сохранить опись')
    expect(s.button.value.label).toBe('Сохранить опись')
    s.saved()
    expect(s.button.value.label).toBe('Сохранено')
  })
})
