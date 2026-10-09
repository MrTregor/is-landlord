// Календарь (FR-09).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components, operations } from '../schema'

type S = components['schemas']
export type CalendarEvent = S['CalendarEventDTO']
export type CalendarQuery = operations['listCalendarEvents']['parameters']['query']

export function useCalendarEvents(params: MaybeRefOrGetter<CalendarQuery | null>) {
  return useQuery({
    queryKey: ['calendar', 'list', params],
    queryFn: () => unwrap(api.GET('/calendar/events', { params: { query: toValue(params)! } })),
    enabled: () => !!toValue(params),
    placeholderData: keepPreviousData,
  })
}

export function useCalendarMutations() {
  const qc = useQueryClient()
  const refresh = () => qc.invalidateQueries({ queryKey: ['calendar'] })
  return {
    create: useMutation({
      mutationFn: (body: S['CalendarEventInput']) => unwrap(api.POST('/calendar/events', { body })),
      onSuccess: refresh,
    }),
    update: useMutation({
      mutationFn: (v: { id: string; body: S['CalendarEventUpdateInput'] }) =>
        unwrap(api.PUT('/calendar/events/{id}', { params: { path: { id: v.id } }, body: v.body })),
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: (id: string) => unwrap(api.DELETE('/calendar/events/{id}', { params: { path: { id } } })),
      onSuccess: refresh,
    }),
    done: useMutation({
      mutationFn: (v: { id: string; done: boolean }) =>
        unwrap(api.POST('/calendar/events/{id}/done', { params: { path: { id: v.id } }, body: { done: v.done } })),
      onSuccess: refresh,
    }),
  }
}
