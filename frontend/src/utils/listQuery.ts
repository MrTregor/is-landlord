// Параметры списка в адресной строке (13 Фронтенд, раздел 3): фильтры, страница, размер, сортировка.
// Тогда работают кнопка «Назад» и ссылки из сводки.
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

export type ListValue = string | string[] | undefined

export interface ListState {
  page: number
  pageSize: number
  sort?: string
  values: Record<string, ListValue>
}

/** Читает и меняет параметры списка; arrays — имена параметров с несколькими значениями. */
export function useListQuery(defaults: Record<string, ListValue> = {}, arrays: string[] = []) {
  const route = useRoute()
  const router = useRouter()

  const state = computed<ListState>(() => {
    const q = route.query
    const values: Record<string, ListValue> = {}
    const keys = new Set([...Object.keys(defaults), ...Object.keys(q)])
    for (const k of keys) {
      if (k === 'page' || k === 'pageSize' || k === 'sort') continue
      const raw = q[k]
      if (arrays.includes(k)) {
        const list = (Array.isArray(raw) ? raw : raw != null ? [raw] : []).filter((v): v is string => !!v)
        values[k] = k in q ? list : (defaults[k] as string[] | undefined)
      } else {
        const v = Array.isArray(raw) ? raw[0] : raw
        values[k] = typeof v === 'string' && v !== '' ? v : k in q ? undefined : (defaults[k] as string | undefined)
      }
    }
    return {
      page: Math.max(1, Number(q.page ?? 1) || 1),
      pageSize: Math.min(100, Number(q.pageSize ?? 25) || 25),
      sort: typeof q.sort === 'string' && q.sort ? q.sort : undefined,
      values,
    }
  })

  /** Изменить параметры; по умолчанию — с возвратом на первую страницу. */
  function set(patch: Record<string, ListValue | number>, resetPage = true) {
    const next: Record<string, string | string[]> = {}
    const merged: Record<string, ListValue | number> = {
      ...state.value.values,
      sort: state.value.sort,
      page: resetPage ? 1 : state.value.page,
      pageSize: state.value.pageSize,
      ...patch,
    }
    for (const [k, v] of Object.entries(merged)) {
      if (v === undefined || v === '' || (k === 'page' && v === 1) || (k === 'pageSize' && v === 25)) continue
      if (Array.isArray(v)) {
        // Пустой список при непустом значении по умолчанию нужно сохранить явно.
        if (v.length || (defaults[k] as string[] | undefined)?.length) next[k] = v.length ? v : ['']
        continue
      }
      next[k] = String(v)
    }
    void router.replace({ query: next })
  }

  function reset() {
    void router.replace({ query: {} })
  }

  return { state, set, reset }
}
