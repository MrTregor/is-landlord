// Сверка (FR-07.2, FR-07.3): выписки банка и их строки.
import { keepPreviousData, useMutation, useQuery } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, API_BASE, getCsrfToken, unwrap } from '../client'
import type { components, operations } from '../schema'
import { ApiError, type Problem } from '@/utils/problem'
import { useMoneyRefresh } from './payments'

type S = components['schemas']
export type BankStatement = S['BankStatementDTO']
export type BankStatementDetail = S['BankStatementDetail']
export type BankLine = S['BankLineDTO']
export type BankStatementsQuery = NonNullable<operations['listBankStatements']['parameters']['query']>

/** Адрес файла-образца CSV. */
export const csvTemplateUrl = `${API_BASE}/bank-statements/csv-template`

export function useBankStatements(params: MaybeRefOrGetter<BankStatementsQuery>) {
  return useQuery({
    queryKey: ['statements', 'list', params],
    queryFn: () => unwrap(api.GET('/bank-statements', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
  })
}

export function useBankStatement(
  id: MaybeRefOrGetter<string>,
  lineStatus: MaybeRefOrGetter<S['BankLineStatus'] | undefined>,
) {
  return useQuery({
    queryKey: ['statements', id, lineStatus],
    queryFn: () =>
      unwrap(
        api.GET('/bank-statements/{id}', {
          params: { path: { id: toValue(id) }, query: { lineStatus: toValue(lineStatus) } },
        }),
      ),
    enabled: () => !!toValue(id),
    placeholderData: keepPreviousData,
  })
}

/**
 * Загрузка выписки POST /bank-statements (multipart). openapi-fetch не собирает multipart из File,
 * поэтому запрос формируется вручную с тем же CSRF-заголовком.
 */
async function uploadStatement(file: File): Promise<BankStatement> {
  const csrf = getCsrfToken()
  const form = new FormData()
  form.append('file', file, file.name)
  const res = await fetch(`${API_BASE}/bank-statements`, {
    method: 'POST',
    body: form,
    credentials: 'include',
    headers: csrf ? { 'X-CSRF-Token': csrf } : {},
  })
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    const problem: Problem =
      body && typeof body === 'object' && 'code' in body
        ? (body as Problem)
        : { type: 'about:blank', title: 'Ошибка загрузки', status: res.status, code: 'unknown' }
    throw new ApiError(problem, res.status)
  }
  return body as BankStatement
}

export function useStatementMutations() {
  const refresh = useMoneyRefresh()
  const byId = (id: string) => ({ params: { path: { id } } })
  return {
    upload: useMutation({ mutationFn: uploadStatement, onSuccess: refresh }),
    samePayment: useMutation({
      mutationFn: (v: { lineId: string; paymentId: string }) =>
        unwrap(
          api.POST('/bank-statement-lines/{id}/same-payment', { ...byId(v.lineId), body: { paymentId: v.paymentId } }),
        ),
      onSuccess: refresh,
    }),
    differentPayment: useMutation({
      mutationFn: (lineId: string) => unwrap(api.POST('/bank-statement-lines/{id}/different-payment', byId(lineId))),
      onSuccess: refresh,
    }),
  }
}
