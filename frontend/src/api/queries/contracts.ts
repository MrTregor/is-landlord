// Договоры, допсоглашения и акты (FR-03).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components, operations } from '../schema'

type S = components['schemas']
export type ContractDetail = S['ContractDetail']
export type ContractInput = S['ContractInput']
export type ContractListItem = S['ContractListItem']
export type Amendment = S['AmendmentDTO']
export type Act = S['ActDTO']
export type ContractsQuery = NonNullable<operations['listContracts']['parameters']['query']>

export function useContracts(params: MaybeRefOrGetter<ContractsQuery>) {
  return useQuery({
    queryKey: ['contracts', 'list', params],
    queryFn: () => unwrap(api.GET('/contracts', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
  })
}

export function useContract(id: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: ['contracts', id],
    queryFn: () => unwrap(api.GET('/contracts/{id}', { params: { path: { id: toValue(id) } } })),
    enabled: () => !!toValue(id),
  })
}

export function useContractSub<T>(id: MaybeRefOrGetter<string>, sub: string, fn: (id: string) => Promise<T>) {
  return useQuery({ queryKey: ['contracts', id, sub], queryFn: () => fn(toValue(id)), enabled: () => !!toValue(id) })
}

export const fetchAmendments = (id: string) =>
  unwrap(api.GET('/contracts/{id}/amendments', { params: { path: { id } } }))
export const fetchActs = (id: string) => unwrap(api.GET('/contracts/{id}/acts', { params: { path: { id } } }))

export function useContractTimeline(id: MaybeRefOrGetter<string>, page: MaybeRefOrGetter<number>) {
  return useQuery({
    queryKey: ['contracts', id, 'timeline', page],
    queryFn: () =>
      unwrap(
        api.GET('/contracts/{id}/timeline', {
          params: { path: { id: toValue(id) }, query: { page: toValue(page), pageSize: 25 } },
        }),
      ),
    placeholderData: keepPreviousData,
  })
}

/** Создание черновика (вне карточки: мастер). */
export function useCreateContract() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: ContractInput) => unwrap(api.POST('/contracts', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['contracts'] }),
  })
}

export function useContractMutations(id: MaybeRefOrGetter<string>) {
  const qc = useQueryClient()
  const path = () => ({ params: { path: { id: toValue(id) } } })
  const refresh = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: ['contracts'] }),
      qc.invalidateQueries({ queryKey: ['properties'] }),
      qc.invalidateQueries({ queryKey: ['tenants'] }),
      qc.invalidateQueries({ queryKey: ['calendar'] }),
    ])
  return {
    update: useMutation({
      mutationFn: (body: ContractInput) => unwrap(api.PUT('/contracts/{id}', { ...path(), body })),
      onSuccess: refresh,
    }),
    patch: useMutation({
      mutationFn: (body: S['ContractPatch']) => unwrap(api.PATCH('/contracts/{id}', { ...path(), body })),
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: () => unwrap(api.DELETE('/contracts/{id}', path())),
      onSuccess: refresh,
    }),
    activate: useMutation({
      mutationFn: (body: S['ContractActivateRequest']) =>
        unwrap(api.POST('/contracts/{id}/activate', { ...path(), body })),
      onSuccess: refresh,
    }),
    terminate: useMutation({
      mutationFn: (body: S['ContractTerminateRequest']) =>
        unwrap(api.POST('/contracts/{id}/terminate', { ...path(), body })),
      onSuccess: refresh,
    }),
    occupants: useMutation({
      mutationFn: (body: S['OccupantsInput']) => unwrap(api.PUT('/contracts/{id}/occupants', { ...path(), body })),
      onSuccess: refresh,
    }),
    createAmendment: useMutation({
      mutationFn: (body: S['AmendmentInput']) => unwrap(api.POST('/contracts/{id}/amendments', { ...path(), body })),
      onSuccess: refresh,
    }),
    updateAmendment: useMutation({
      mutationFn: (v: { id: string; body: S['AmendmentInput'] }) =>
        unwrap(api.PUT('/amendments/{id}', { params: { path: { id: v.id } }, body: v.body })),
      onSuccess: refresh,
    }),
    signAmendment: useMutation({
      mutationFn: (v: { id: string; signedOn: string }) =>
        unwrap(api.POST('/amendments/{id}/sign', { params: { path: { id: v.id } }, body: { signedOn: v.signedOn } })),
      onSuccess: refresh,
    }),
    cancelAmendment: useMutation({
      mutationFn: (v: { id: string; reason: string }) =>
        unwrap(api.POST('/amendments/{id}/cancel', { params: { path: { id: v.id } }, body: { reason: v.reason } })),
      onSuccess: refresh,
    }),
    createReturnAct: useMutation({
      mutationFn: () => unwrap(api.POST('/contracts/{id}/acts', { ...path(), body: { kind: 'return' } })),
      onSuccess: refresh,
    }),
    updateAct: useMutation({
      mutationFn: (v: { id: string; body: S['ActInput'] }) =>
        unwrap(api.PUT('/acts/{id}', { params: { path: { id: v.id } }, body: v.body })),
      onSuccess: refresh,
    }),
    signAct: useMutation({
      mutationFn: (v: { id: string; signedOn: string }) =>
        unwrap(api.POST('/acts/{id}/sign', { params: { path: { id: v.id } }, body: { signedOn: v.signedOn } })),
      onSuccess: refresh,
    }),
  }
}
