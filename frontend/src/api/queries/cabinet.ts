// Личный кабинет нанимателя (FR-13, 06 разд. 15): сводка, начисления, платежи, онлайн-оплата.
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components } from '../schema'

type S = components['schemas']
export type CabinetSummary = S['CabinetSummary']
export type CabinetCharge = S['CabinetChargeDTO']
export type CabinetPayment = S['CabinetPaymentDTO']

export function useCabinetSummary(contractId: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: ['cabinet', 'summary', contractId],
    queryFn: () => unwrap(api.GET('/cabinet/summary', { params: { query: { contractId: toValue(contractId) } } })),
  })
}

export function useCabinetCharges(contractId: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: ['cabinet', 'charges', contractId],
    queryFn: () => unwrap(api.GET('/cabinet/charges', { params: { query: { contractId: toValue(contractId)! } } })),
    enabled: () => !!toValue(contractId),
  })
}

export function useCabinetPayments(contractId: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: ['cabinet', 'payments', contractId],
    queryFn: () => unwrap(api.GET('/cabinet/payments', { params: { query: { contractId: toValue(contractId)! } } })),
    enabled: () => !!toValue(contractId),
  })
}

/**
 * Статус платежа для страницы возврата из оплаты (15 разд. 3): опрос каждые 2 с, пока платёж pending
 * и не истекло время ожидания (stopped).
 */
export function useCabinetPaymentStatus(id: MaybeRefOrGetter<string>, stopped: MaybeRefOrGetter<boolean>) {
  return useQuery({
    queryKey: ['cabinet', 'payment', id],
    queryFn: () => unwrap(api.GET('/cabinet/payments/{id}', { params: { path: { id: toValue(id) } } })),
    refetchInterval: (q) => {
      const data = q.state.data
      return !toValue(stopped) && (!data || data.status === 'pending') ? 2000 : false
    },
  })
}

/** Начать онлайн-оплату и вернуть адрес страницы оплаты банка. */
export function useStartPayment() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: S['CabinetPaymentInput']) => unwrap(api.POST('/cabinet/payments', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['cabinet'] }),
  })
}

/** Сводка договора без кэша (пересчёт состава оплаты перед повтором). */
export const fetchCabinetSummary = (contractId: string) =>
  unwrap(api.GET('/cabinet/summary', { params: { query: { contractId } } }))

/** Квитанция в кабинете. */
export const cabinetReceiptUrl = (id: string) => `/api/v1/cabinet/payments/${id}/receipt.pdf`

// ---------------------------------------------------------------- M13: показания, заявки, договор, профиль

export type CabinetMeters = S['CabinetMeters']
export type CabinetMeter = S['CabinetMeter']
export type CabinetContract = S['CabinetContract']
export type CabinetProfile = S['CabinetProfile']
export type CabinetRequestItem = S['CabinetRequestListItem']
export type CabinetRequestDetail = S['CabinetRequestDetail']

export function useCabinetMeters(contractId: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: ['cabinet', 'meters', contractId],
    queryFn: () => unwrap(api.GET('/cabinet/meters', { params: { query: { contractId: toValue(contractId)! } } })),
    enabled: () => !!toValue(contractId),
  })
}

/** Передать показания по всем счётчикам объекта (15 разд. 5). */
export function useSubmitReadings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: S['CabinetReadingsInput']) => unwrap(api.POST('/cabinet/meter-readings', { body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['cabinet'] }),
  })
}

export function useCabinetContract(id: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: ['cabinet', 'contract', id],
    queryFn: () => unwrap(api.GET('/cabinet/contracts/{id}', { params: { path: { id: toValue(id)! } } })),
    enabled: () => !!toValue(id),
  })
}

export function useCabinetRequests(contractId: MaybeRefOrGetter<string | undefined>) {
  return useQuery({
    queryKey: ['cabinet', 'requests', contractId],
    queryFn: () => unwrap(api.GET('/cabinet/requests', { params: { query: { contractId: toValue(contractId)! } } })),
    enabled: () => !!toValue(contractId),
  })
}

export function useCabinetRequest(id: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: ['cabinet', 'request', id],
    queryFn: () => unwrap(api.GET('/cabinet/requests/{id}', { params: { path: { id: toValue(id) } } })),
    enabled: () => !!toValue(id),
  })
}

export function useCabinetRequestMutations() {
  const qc = useQueryClient()
  const done = () => qc.invalidateQueries({ queryKey: ['cabinet'] })
  const byId = (id: string) => ({ params: { path: { id } } })
  return {
    create: useMutation({
      mutationFn: (body: S['CabinetRequestInput']) => unwrap(api.POST('/cabinet/requests', { body })),
      onSuccess: done,
    }),
    comment: useMutation({
      mutationFn: (v: { id: string; text: string }) =>
        unwrap(api.POST('/cabinet/requests/{id}/comments', { ...byId(v.id), body: { text: v.text } })),
      onSuccess: done,
    }),
    confirm: useMutation({
      mutationFn: (id: string) => unwrap(api.POST('/cabinet/requests/{id}/confirm', byId(id))),
      onSuccess: done,
    }),
    reopen: useMutation({
      mutationFn: (v: { id: string; comment: string }) =>
        unwrap(api.POST('/cabinet/requests/{id}/reopen', { ...byId(v.id), body: { comment: v.comment } })),
      onSuccess: done,
    }),
  }
}

export function useCabinetProfile() {
  return useQuery({
    queryKey: ['cabinet', 'profile'],
    queryFn: () => unwrap(api.GET('/cabinet/profile')),
  })
}

export function useCabinetProfileMutations() {
  const qc = useQueryClient()
  const done = () => qc.invalidateQueries({ queryKey: ['cabinet', 'profile'] })
  return {
    phone: useMutation({
      mutationFn: (phone: string | null) => unwrap(api.PATCH('/cabinet/profile', { body: { phone } })),
      onSuccess: done,
    }),
    consent: useMutation({
      mutationFn: () => unwrap(api.POST('/cabinet/consent', { body: { agree: true } })),
      onSuccess: done,
    }),
    revoke: useMutation({
      mutationFn: () => unwrap(api.POST('/cabinet/consent/revoke')),
      onSuccess: done,
    }),
  }
}
