// Чеки (FR-06.2, FR-06.4): список, повтор отправки, ссылка на чек НПД.
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components, operations } from '../schema'

type S = components['schemas']
export type FiscalReceipt = S['FiscalReceiptDTO']
export type FiscalQuery = NonNullable<operations['listFiscalReceipts']['parameters']['query']>

/** Чек в работе у кассы: список обновляется сам, пока такие есть на странице. */
const inProgress = (items: FiscalReceipt[] | undefined) =>
  (items ?? []).some((r) => r.status === 'queued' || r.status === 'wait')

export function useFiscalReceipts(params: MaybeRefOrGetter<FiscalQuery>) {
  return useQuery({
    queryKey: ['fiscal', 'list', params],
    queryFn: () => unwrap(api.GET('/fiscal-receipts', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => (inProgress(q.state.data?.items) ? 3000 : false),
  })
}

export function useFiscalMutations() {
  const qc = useQueryClient()
  const refresh = () =>
    Promise.all(['fiscal', 'payments', 'dashboard'].map((k) => qc.invalidateQueries({ queryKey: [k] })))
  const byId = (id: string) => ({ params: { path: { id } } })
  return {
    retry: useMutation({
      mutationFn: (id: string) => unwrap(api.POST('/fiscal-receipts/{id}/retry', byId(id))),
      onSuccess: refresh,
    }),
    manual: useMutation({
      mutationFn: (v: { id: string; npdReceiptUrl: string }) =>
        unwrap(api.POST('/fiscal-receipts/{id}/manual', { ...byId(v.id), body: { npdReceiptUrl: v.npdReceiptUrl } })),
      onSuccess: refresh,
    }),
  }
}
