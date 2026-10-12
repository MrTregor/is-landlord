// Начисления (FR-05) и показания счётчиков (FR-05.5).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components, operations } from '../schema'

type S = components['schemas']
export type Charge = S['ChargeDTO']
export type ChargeDetail = S['ChargeDetail']
export type ChargesQuery = NonNullable<operations['listCharges']['parameters']['query']>
export type Reading = S['MeterReadingDTO']
export type ReadingsQuery = NonNullable<operations['listMeterReadings']['parameters']['query']>

/** Список начислений; enabled — загружать ли (например, только при открытом диалоге). */
export function useCharges(params: MaybeRefOrGetter<ChargesQuery>, enabled: MaybeRefOrGetter<boolean> = true) {
  return useQuery({
    queryKey: ['charges', 'list', params],
    queryFn: () => unwrap(api.GET('/charges', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
    enabled: () => toValue(enabled),
  })
}

export function useCharge(id: MaybeRefOrGetter<string | null>) {
  return useQuery({
    queryKey: ['charges', id],
    queryFn: () => unwrap(api.GET('/charges/{id}', { params: { path: { id: toValue(id)! } } })),
    enabled: () => !!toValue(id),
  })
}

/** После изменения начислений обновляются списки, карточки договоров, сводки и календарь. */
function useRefresh() {
  const qc = useQueryClient()
  return () =>
    Promise.all(
      ['charges', 'contracts', 'properties', 'tenants', 'calendar', 'readings'].map((k) =>
        qc.invalidateQueries({ queryKey: [k] }),
      ),
    )
}

export function useChargeMutations() {
  const refresh = useRefresh()
  const byId = (id: string) => ({ params: { path: { id } } })
  return {
    create: useMutation({
      mutationFn: (body: S['ChargeCreateInput']) => unwrap(api.POST('/charges', { body })),
      onSuccess: refresh,
    }),
    adjust: useMutation({
      mutationFn: (v: { id: string; body: S['ChargeAdjustInput'] }) =>
        unwrap(api.POST('/charges/{id}/adjust', { ...byId(v.id), body: v.body })),
      onSuccess: refresh,
    }),
    cancel: useMutation({
      mutationFn: (v: { id: string; reason: string }) =>
        unwrap(api.POST('/charges/{id}/cancel', { ...byId(v.id), body: { reason: v.reason } })),
      onSuccess: refresh,
    }),
    setPenalty: useMutation({
      mutationFn: (v: { id: string; body: S['PenaltySetInput'] }) =>
        unwrap(api.POST('/charges/{id}/penalty-set', { ...byId(v.id), body: v.body })),
      onSuccess: refresh,
    }),
    writeOff: useMutation({
      mutationFn: (v: { id: string; reason: string }) =>
        unwrap(api.POST('/charges/{id}/penalty-write-off', { ...byId(v.id), body: { reason: v.reason } })),
      onSuccess: refresh,
    }),
    utilities: useMutation({
      mutationFn: (v: { contractId: string; periodMonth: string }) =>
        unwrap(
          api.POST('/contracts/{id}/utilities-charge', {
            params: { path: { id: v.contractId } },
            body: { periodMonth: v.periodMonth },
          }),
        ),
      onSuccess: refresh,
    }),
  }
}

export function useReadings(params: MaybeRefOrGetter<ReadingsQuery>) {
  return useQuery({
    queryKey: ['readings', 'list', params],
    queryFn: () => unwrap(api.GET('/meter-readings', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
  })
}

export function useReadingMutations() {
  const refresh = useRefresh()
  return {
    create: useMutation({
      mutationFn: (body: S['MeterReadingInput']) => unwrap(api.POST('/meter-readings', { body })),
      onSuccess: refresh,
    }),
    accept: useMutation({
      mutationFn: (id: string) => unwrap(api.POST('/meter-readings/{id}/accept', { params: { path: { id } } })),
      onSuccess: refresh,
    }),
    reject: useMutation({
      mutationFn: (v: { id: string; reason: string }) =>
        unwrap(api.POST('/meter-readings/{id}/reject', { params: { path: { id: v.id } }, body: { reason: v.reason } })),
      onSuccess: refresh,
    }),
  }
}
