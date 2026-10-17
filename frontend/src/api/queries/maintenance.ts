// Заявки на обслуживание (FR-08) и затраты (FR-05.6).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components, operations } from '../schema'
import { useMoneyRefresh } from './payments'

type S = components['schemas']
export type RequestItem = S['MaintenanceRequestListItem']
export type RequestDetail = S['MaintenanceRequestDetail']
export type RequestEvent = S['RequestEventDTO']
export type RequestsQuery = NonNullable<operations['listMaintenanceRequests']['parameters']['query']>
export type Expense = S['ExpenseDTO']
export type ExpensesQuery = NonNullable<operations['listExpenses']['parameters']['query']>

export function useRequests(params: MaybeRefOrGetter<RequestsQuery>, enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: ['requests', 'list', params],
    queryFn: () => unwrap(api.GET('/maintenance-requests', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
    enabled: () => toValue(enabled),
  })
}

export function useRequest(id: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: ['requests', id],
    queryFn: () => unwrap(api.GET('/maintenance-requests/{id}', { params: { path: { id: toValue(id) } } })),
    enabled: () => !!toValue(id),
  })
}

export function useRequestMutations() {
  const qc = useQueryClient()
  const done = () =>
    Promise.all(['requests', 'properties', 'dashboard'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
  const byId = (id: string) => ({ params: { path: { id } } })
  return {
    create: useMutation({
      mutationFn: (body: S['MaintenanceRequestInput']) => unwrap(api.POST('/maintenance-requests', { body })),
      onSuccess: done,
    }),
    update: useMutation({
      mutationFn: (v: { id: string; body: S['MaintenanceRequestPatch'] }) =>
        unwrap(api.PATCH('/maintenance-requests/{id}', { ...byId(v.id), body: v.body })),
      onSuccess: done,
    }),
    transition: useMutation({
      mutationFn: (v: { id: string; body: S['RequestTransitionInput'] }) =>
        unwrap(api.POST('/maintenance-requests/{id}/transition', { ...byId(v.id), body: v.body })),
      onSuccess: done,
    }),
    comment: useMutation({
      mutationFn: (v: { id: string; text: string }) =>
        unwrap(api.POST('/maintenance-requests/{id}/comments', { ...byId(v.id), body: { text: v.text } })),
      onSuccess: done,
    }),
  }
}

export function useExpenses(params: MaybeRefOrGetter<ExpensesQuery>, enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: ['expenses', 'list', params],
    queryFn: () => unwrap(api.GET('/expenses', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
    enabled: () => toValue(enabled),
  })
}

/** После изменения затрат обновляются затраты, заявки, объекты и (при возмещении) деньги договора. */
export function useExpenseMutations() {
  const qc = useQueryClient()
  const money = useMoneyRefresh()
  const done = () =>
    Promise.all([money(), ...['expenses', 'requests'].map((k) => qc.invalidateQueries({ queryKey: [k] }))])
  const byId = (id: string) => ({ params: { path: { id } } })
  return {
    create: useMutation({
      mutationFn: (body: S['ExpenseCreateInput']) => unwrap(api.POST('/expenses', { body })),
      onSuccess: done,
    }),
    update: useMutation({
      mutationFn: (v: { id: string; body: S['ExpenseUpdateInput'] }) =>
        unwrap(api.PUT('/expenses/{id}', { ...byId(v.id), body: v.body })),
      onSuccess: done,
    }),
    remove: useMutation({
      mutationFn: (id: string) => unwrap(api.DELETE('/expenses/{id}', byId(id))),
      onSuccess: done,
    }),
  }
}
