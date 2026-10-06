// Сводка: показатели и счётчики для значков меню (обновление раз в 60 с).
import { useQuery } from '@tanstack/vue-query'
import { api, unwrap } from '../client'

export function useDashboard() {
  return useQuery({
    queryKey: ['dashboard'],
    queryFn: () => unwrap(api.GET('/dashboard')),
    refetchInterval: 60_000,
    retry: false,
  })
}
