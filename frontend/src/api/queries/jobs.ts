// Плановые задачи (Настройки → Служебное).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components } from '../schema'

export type JobName = components['schemas']['JobName']

export function useJobs() {
  return useQuery({ queryKey: ['jobs'], queryFn: () => unwrap(api.GET('/settings/jobs')), refetchInterval: 5000 })
}

export function useJobRuns(name: MaybeRefOrGetter<JobName | undefined>, page: MaybeRefOrGetter<number>) {
  return useQuery({
    queryKey: ['jobs', 'runs', name, page],
    queryFn: () =>
      unwrap(
        api.GET('/settings/jobs/runs', {
          params: { query: { name: toValue(name), page: toValue(page), pageSize: 25 } },
        }),
      ),
    placeholderData: keepPreviousData,
    refetchInterval: 5000,
  })
}

export function useRunJob() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: JobName) => unwrap(api.POST('/settings/jobs/{name}/run', { params: { path: { name } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['jobs'] }),
  })
}
