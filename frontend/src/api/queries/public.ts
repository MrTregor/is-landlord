// Общедоступные сведения: название системы и организации, режим работы.
import { useQuery } from '@tanstack/vue-query'
import { api, unwrap } from '../client'

export function useAppInfo() {
  return useQuery({
    queryKey: ['public', 'app-info'],
    queryFn: () => unwrap(api.GET('/public/app-info')),
    staleTime: 5 * 60_000,
    retry: false,
  })
}
