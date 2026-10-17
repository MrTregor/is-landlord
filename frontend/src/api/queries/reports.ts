// Отчёты (FR-11), сводка и доходность объекта: данные на момент запроса; XLSX — ссылками на те же параметры.
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, API_BASE, unwrap } from '../client'
import type { components, operations } from '../schema'

type S = components['schemas']
export type IncomeExpenseReport = S['IncomeExpenseReport']
export type OccupancyReport = S['OccupancyReport']
export type DebtsReport = S['DebtsReport']
export type TaxBaseReport = S['TaxBaseReport']
export type IncomeBookReport = S['IncomeBookReport']
export type Profitability = S['PropertyProfitability']
type Q<T extends keyof operations> = NonNullable<operations[T]['parameters']['query']>

/** Адрес выгрузки XLSX с параметрами (скачивание по ссылке — GET с cookie сессии). */
export function xlsxUrl(path: string, params: Record<string, string | number | boolean | string[] | null | undefined>) {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue
    if (Array.isArray(v)) v.forEach((x) => q.append(k, x))
    else q.append(k, String(v))
  }
  const s = q.toString()
  return `${API_BASE}${path}${s ? `?${s}` : ''}`
}

const opts = { placeholderData: keepPreviousData, staleTime: 0 }

export function useIncomeExpense(p: MaybeRefOrGetter<Q<'getIncomeExpenseReport'> | null>) {
  return useQuery({
    queryKey: ['reports', 'income-expense', p],
    queryFn: () => unwrap(api.GET('/reports/income-expense', { params: { query: toValue(p)! } })),
    enabled: () => !!toValue(p),
    ...opts,
  })
}

export function useOccupancy(p: MaybeRefOrGetter<Q<'getOccupancyReport'> | null>) {
  return useQuery({
    queryKey: ['reports', 'occupancy', p],
    queryFn: () => unwrap(api.GET('/reports/occupancy', { params: { query: toValue(p)! } })),
    enabled: () => !!toValue(p),
    ...opts,
  })
}

export function useDebts(p: MaybeRefOrGetter<Q<'getDebtsReport'> | null>) {
  return useQuery({
    queryKey: ['reports', 'debts', p],
    queryFn: () => unwrap(api.GET('/reports/debts', { params: { query: toValue(p)! } })),
    enabled: () => !!toValue(p),
    ...opts,
  })
}

export function useTaxBase(p: MaybeRefOrGetter<Q<'getTaxBaseReport'> | null>) {
  return useQuery({
    queryKey: ['reports', 'tax-base', p],
    queryFn: () => unwrap(api.GET('/reports/tax-base', { params: { query: toValue(p)! } })),
    enabled: () => !!toValue(p),
    ...opts,
  })
}

export function useIncomeBook(p: MaybeRefOrGetter<Q<'getIncomeBookReport'> | null>) {
  return useQuery({
    queryKey: ['reports', 'income-book', p],
    queryFn: () => unwrap(api.GET('/reports/income-book', { params: { query: toValue(p)! } })),
    enabled: () => !!toValue(p),
    ...opts,
  })
}

export function useProfitability(id: MaybeRefOrGetter<string>, p: MaybeRefOrGetter<{ from: string; to: string }>) {
  return useQuery({
    queryKey: ['properties', id, 'profitability', p],
    queryFn: () =>
      unwrap(api.GET('/properties/{id}/profitability', { params: { path: { id: toValue(id) }, query: toValue(p) } })),
    ...opts,
  })
}
