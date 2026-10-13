// Платежи (FR-06.1), обеспечительный платёж (FR-05.2), взаиморасчёт и расхождения (FR-07.3).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components, operations } from '../schema'

type S = components['schemas']
export type Payment = S['PaymentDTO']
export type PaymentDetail = S['PaymentDetail']
export type PaymentsQuery = NonNullable<operations['listPayments']['parameters']['query']>
export type Deposit = S['DepositDTO']
export type DepositOperation = S['DepositOperationDTO']
export type Settlement = S['ContractSettlement']
export type Discrepancy = S['DiscrepancyDTO']
export type DiscrepanciesQuery = NonNullable<operations['listDiscrepancies']['parameters']['query']>

/** Адрес квитанции PDF платежа. */
export const receiptPdfUrl = (id: string) => `/api/v1/payments/${id}/receipt.pdf`

export function usePayments(params: MaybeRefOrGetter<PaymentsQuery>) {
  return useQuery({
    queryKey: ['payments', 'list', params],
    queryFn: () => unwrap(api.GET('/payments', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
  })
}

export function usePayment(id: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: ['payments', id],
    queryFn: () => unwrap(api.GET('/payments/{id}', { params: { path: { id: toValue(id) } } })),
    enabled: () => !!toValue(id),
  })
}

export function useDeposit(contractId: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: ['contracts', contractId, 'deposit'],
    queryFn: () => unwrap(api.GET('/contracts/{id}/deposit', { params: { path: { id: toValue(contractId) } } })),
    enabled: () => !!toValue(contractId),
  })
}

export function useSettlement(contractId: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: ['contracts', contractId, 'settlement'],
    queryFn: () => unwrap(api.GET('/contracts/{id}/settlement', { params: { path: { id: toValue(contractId) } } })),
    enabled: () => !!toValue(contractId),
  })
}

export function useDiscrepancies(params: MaybeRefOrGetter<DiscrepanciesQuery>) {
  return useQuery({
    queryKey: ['discrepancies', 'list', params],
    queryFn: () => unwrap(api.GET('/discrepancies', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
  })
}

/** После операций с деньгами обновляются платежи, начисления, договоры (залог, взаиморасчёт), расхождения, сводка. */
function useRefresh() {
  const qc = useQueryClient()
  return () =>
    Promise.all(
      ['payments', 'charges', 'contracts', 'discrepancies', 'properties', 'tenants', 'calendar', 'dashboard'].map((k) =>
        qc.invalidateQueries({ queryKey: [k] }),
      ),
    )
}

export function usePaymentMutations() {
  const refresh = useRefresh()
  const byId = (id: string) => ({ params: { path: { id } } })
  return {
    create: useMutation({
      mutationFn: (body: S['PaymentCreateInput']) => unwrap(api.POST('/payments', { body })),
      onSuccess: refresh,
    }),
    cancel: useMutation({
      mutationFn: (v: { id: string; reason: string }) =>
        unwrap(api.POST('/payments/{id}/cancel', { ...byId(v.id), body: { reason: v.reason } })),
      onSuccess: refresh,
    }),
    refund: useMutation({
      mutationFn: (v: { id: string; body: S['RefundInput'] }) =>
        unwrap(api.POST('/payments/{id}/refunds', { ...byId(v.id), body: v.body })),
      onSuccess: refresh,
    }),
    checkStatus: useMutation({
      mutationFn: (id: string) => unwrap(api.POST('/payments/{id}/check-status', byId(id))),
      onSuccess: refresh,
    }),
    moveToDeposit: useMutation({
      mutationFn: (v: { id: string; body: S['AdvanceToDepositInput'] }) =>
        unwrap(api.POST('/payments/{id}/move-to-deposit', { ...byId(v.id), body: v.body })),
      onSuccess: refresh,
    }),
  }
}

export function useDepositMutations() {
  const refresh = useRefresh()
  const byId = (id: string) => ({ params: { path: { id } } })
  return {
    withhold: useMutation({
      mutationFn: (v: { contractId: string; body: S['DepositWithholdingInput'] }) =>
        unwrap(api.POST('/contracts/{id}/deposit/withholdings', { ...byId(v.contractId), body: v.body })),
      onSuccess: refresh,
    }),
    offset: useMutation({
      mutationFn: (v: { contractId: string; body: S['DepositOffsetInput'] }) =>
        unwrap(api.POST('/contracts/{id}/deposit/offsets', { ...byId(v.contractId), body: v.body })),
      onSuccess: refresh,
    }),
    refund: useMutation({
      mutationFn: (v: { contractId: string; body: S['DepositRefundInput'] }) =>
        unwrap(api.POST('/contracts/{id}/deposit/refunds', { ...byId(v.contractId), body: v.body })),
      onSuccess: refresh,
    }),
    cancel: useMutation({
      mutationFn: (v: { id: string; reason: string }) =>
        unwrap(api.POST('/deposit-operations/{id}/cancel', { ...byId(v.id), body: { reason: v.reason } })),
      onSuccess: refresh,
    }),
  }
}

export function useDiscrepancyMutations() {
  const refresh = useRefresh()
  return {
    resolve: useMutation({
      mutationFn: (v: { id: string; body: S['DiscrepancyResolveInput'] }) =>
        unwrap(api.POST('/discrepancies/{id}/resolve', { params: { path: { id: v.id } }, body: v.body })),
      onSuccess: refresh,
    }),
  }
}
