// Настройки организации и пользователи (FR-14.1).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import type { MaybeRefOrGetter } from 'vue'
import { toValue } from 'vue'
import { api, unwrap } from '../client'
import type { components } from '../schema'

export type OrganizationSettings = components['schemas']['OrganizationSettings']
export type OrganizationSettingsInput = components['schemas']['OrganizationSettingsInput']

export function useOrganizationSettings() {
  return useQuery({
    queryKey: ['settings', 'organization'],
    queryFn: () => unwrap(api.GET('/settings/organization')),
  })
}

/** Сохранить настройки: на сервер уходит полный объект, изменённые поля вкладки накладываются на загруженные. */
export function useSaveOrganizationSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: OrganizationSettingsInput) => unwrap(api.PUT('/settings/organization', { body })),
    onSuccess: (data) => {
      qc.setQueryData(['settings', 'organization'], data)
      void qc.invalidateQueries({ queryKey: ['public'] })
    },
  })
}

/** Убирает служебные поля ответа, оставляя тело запроса. */
export function toInput(s: OrganizationSettings): OrganizationSettingsInput {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { updatedAt, updatedBy, ...input } = s
  return input
}

export interface UsersParams {
  page: number
  pageSize: number
}

export function useUsers(params: MaybeRefOrGetter<UsersParams>) {
  return useQuery({
    queryKey: ['users', 'list', params],
    queryFn: () => unwrap(api.GET('/users', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
  })
}

export function useUserMutations() {
  const qc = useQueryClient()
  const done = () => qc.invalidateQueries({ queryKey: ['users'] })
  return {
    create: useMutation({
      mutationFn: (body: components['schemas']['UserCreateInput']) => unwrap(api.POST('/users', { body })),
      onSuccess: done,
    }),
    update: useMutation({
      mutationFn: (v: { id: string; body: components['schemas']['UserUpdateInput'] }) =>
        unwrap(api.PATCH('/users/{id}', { params: { path: { id: v.id } }, body: v.body })),
      onSuccess: done,
    }),
    resendInvite: useMutation({
      mutationFn: (id: string) => unwrap(api.POST('/users/{id}/resend-invite', { params: { path: { id } } })),
      onSuccess: done,
    }),
  }
}
