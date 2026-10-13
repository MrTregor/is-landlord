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

/** Статус платежа для страницы возврата из оплаты: опрос каждые 2 с, пока платёж не завершён (15 разд. 3). */
export function useCabinetPaymentStatus(id: MaybeRefOrGetter<string>, polling: MaybeRefOrGetter<boolean>) {
  return useQuery({
    queryKey: ['cabinet', 'payment', id],
    queryFn: () => unwrap(api.GET('/cabinet/payments/{id}', { params: { path: { id: toValue(id) } } })),
    refetchInterval: () => (toValue(polling) ? 2000 : false),
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

/** Квитанция в кабинете. */
export const cabinetReceiptUrl = (id: string) => `/api/v1/cabinet/payments/${id}/receipt.pdf`
