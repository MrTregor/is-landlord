// Состояние кнопки «Сохранить» формы: после успешного сохранения — «Сохранено» с галочкой, после ошибки сервера —
// «Ошибка, попробуйте ещё раз» с крестиком; в обычном состоянии значка нет. Кнопка возвращается к «Сохранить», когда значения формы снова изменились
// (повторная загрузка тех же значений после сохранения состояние не сбрасывает).
import { computed, ref, watch } from 'vue'

export type SaveState = 'idle' | 'saved' | 'failed'

/** values — значения формы, label — подпись кнопки в обычном состоянии. */
export function useSaveState(values: () => unknown, label = 'Сохранить') {
  const state = ref<SaveState>('idle')
  let snapshot = ''
  const current = () => JSON.stringify(values())

  watch(current, (v) => {
    if (state.value !== 'idle' && v !== snapshot) state.value = 'idle'
  })

  function mark(s: SaveState) {
    state.value = s
    snapshot = current()
  }

  const button = computed(() => {
    switch (state.value) {
      case 'saved':
        return { label: 'Сохранено', icon: 'pi pi-check', severity: 'success' as const }
      case 'failed':
        return { label: 'Ошибка, попробуйте ещё раз', icon: 'pi pi-times', severity: 'danger' as const }
      default:
        return { label, icon: undefined, severity: undefined }
    }
  })

  return {
    state,
    button,
    saved: () => mark('saved'),
    failed: () => mark('failed'),
    reset: () => (state.value = 'idle'),
  }
}
