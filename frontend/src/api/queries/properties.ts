// Объекты, счётчики, тарифы, файлы (FR-01).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, API_BASE, getCsrfToken, unwrap } from '../client'
import type { components, operations } from '../schema'
import { ApiError, type Problem } from '@/utils/problem'

type S = components['schemas']
export type PropertyDetail = S['PropertyDetail']
export type PropertyListItem = S['PropertyListItem']
export type PropertyInput = S['PropertyInput']
export type Meter = S['MeterDTO']
export type Tariff = S['TariffDTO']
export type PropertiesQuery = NonNullable<operations['listProperties']['parameters']['query']>

export function useProperties(params: MaybeRefOrGetter<PropertiesQuery>) {
  return useQuery({
    queryKey: ['properties', 'list', params],
    queryFn: () => unwrap(api.GET('/properties', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
  })
}

export function useProperty(id: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: ['properties', id],
    queryFn: () => unwrap(api.GET('/properties/{id}', { params: { path: { id: toValue(id) } } })),
    enabled: () => !!toValue(id),
  })
}

export function usePropertySub<T>(id: MaybeRefOrGetter<string>, sub: string, fn: (id: string) => Promise<T>) {
  return useQuery({ queryKey: ['properties', id, sub], queryFn: () => fn(toValue(id)) })
}

export const fetchStatusHistory = (id: string) =>
  unwrap(api.GET('/properties/{id}/status-history', { params: { path: { id } } }))
export const fetchInventory = (id: string) =>
  unwrap(api.GET('/properties/{id}/inventory', { params: { path: { id } } }))
export const fetchMeters = (id: string) => unwrap(api.GET('/properties/{id}/meters', { params: { path: { id } } }))

/** Мутации карточки объекта; после каждой — обновление объекта и его подресурсов. */
export function usePropertyMutations(id: MaybeRefOrGetter<string>) {
  const qc = useQueryClient()
  const path = () => ({ params: { path: { id: toValue(id) } } })
  const refresh = () => qc.invalidateQueries({ queryKey: ['properties'] })
  return {
    update: useMutation({
      mutationFn: (body: PropertyInput) => unwrap(api.PUT('/properties/{id}', { ...path(), body })),
      onSuccess: refresh,
    }),
    archive: useMutation({
      mutationFn: (archive: boolean) =>
        archive
          ? unwrap(api.POST('/properties/{id}/archive', path()))
          : unwrap(api.POST('/properties/{id}/unarchive', path())),
      onSuccess: refresh,
    }),
    status: useMutation({
      mutationFn: (body: S['PropertyStatusChange']) => unwrap(api.POST('/properties/{id}/status', { ...path(), body })),
      onSuccess: refresh,
    }),
    inventory: useMutation({
      mutationFn: (body: S['PropertyInventoryInput']) =>
        unwrap(api.PUT('/properties/{id}/inventory', { ...path(), body })),
      onSuccess: refresh,
    }),
    addPhoto: useMutation({
      mutationFn: (body: S['PropertyPhotoInput']) => unwrap(api.POST('/properties/{id}/photos', { ...path(), body })),
      onSuccess: refresh,
    }),
    reorderPhotos: useMutation({
      mutationFn: (fileIds: string[]) =>
        unwrap(api.PUT('/properties/{id}/photos/order', { ...path(), body: { fileIds } })),
      onSuccess: refresh,
    }),
    deletePhoto: useMutation({
      mutationFn: (fileId: string) =>
        unwrap(api.DELETE('/properties/{id}/photos/{fileId}', { params: { path: { id: toValue(id), fileId } } })),
      onSuccess: refresh,
    }),
    createMeter: useMutation({
      mutationFn: (body: S['MeterInput']) => unwrap(api.POST('/properties/{id}/meters', { ...path(), body })),
      onSuccess: refresh,
    }),
    updateMeter: useMutation({
      mutationFn: (v: { meterId: string; body: S['MeterInput'] }) =>
        unwrap(api.PUT('/meters/{id}', { params: { path: { id: v.meterId } }, body: v.body })),
      onSuccess: refresh,
    }),
    archiveMeter: useMutation({
      mutationFn: (meterId: string) => unwrap(api.POST('/meters/{id}/archive', { params: { path: { id: meterId } } })),
      onSuccess: refresh,
    }),
  }
}

export function useTariffs() {
  return useQuery({ queryKey: ['tariffs'], queryFn: () => unwrap(api.GET('/settings/tariffs')) })
}

export function useTariffMutations() {
  const qc = useQueryClient()
  const refresh = () => qc.invalidateQueries({ queryKey: ['tariffs'] })
  return {
    create: useMutation({
      mutationFn: (body: S['TariffInput']) => unwrap(api.POST('/settings/tariffs', { body })),
      onSuccess: refresh,
    }),
    remove: useMutation({
      mutationFn: (id: string) => unwrap(api.DELETE('/settings/tariffs/{id}', { params: { path: { id } } })),
      onSuccess: refresh,
    }),
  }
}

/**
 * Загрузка файла POST /files (multipart). openapi-fetch не собирает multipart из File,
 * поэтому запрос формируется вручную с тем же CSRF-заголовком.
 */
export async function uploadFile(file: File, category: S['FileCategory']): Promise<S['FileDTO']> {
  const csrf = getCsrfToken()
  const form = new FormData()
  form.append('category', category)
  form.append('file', file, file.name)
  const res = await fetch(`${API_BASE}/files`, {
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
  return body as S['FileDTO']
}
