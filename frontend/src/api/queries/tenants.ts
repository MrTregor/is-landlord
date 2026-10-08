// Наниматели (FR-02).
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components, operations } from '../schema'

type S = components['schemas']
export type TenantDetail = S['TenantDetail']
export type TenantInput = S['TenantInput']
export type TenantsQuery = NonNullable<operations['listTenants']['parameters']['query']>

export function useTenants(params: MaybeRefOrGetter<TenantsQuery>) {
  return useQuery({
    queryKey: ['tenants', 'list', params],
    queryFn: () => unwrap(api.GET('/tenants', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
  })
}

export function useTenant(id: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: ['tenants', id],
    queryFn: () => unwrap(api.GET('/tenants/{id}', { params: { path: { id: toValue(id) } } })),
    enabled: () => !!toValue(id),
  })
}

export function useTenantMutations(id: MaybeRefOrGetter<string>) {
  const qc = useQueryClient()
  const path = () => ({ params: { path: { id: toValue(id) } } })
  const refresh = () => qc.invalidateQueries({ queryKey: ['tenants'] })
  return {
    reveal: useMutation({ mutationFn: () => unwrap(api.POST('/tenants/{id}/reveal-pdn', path())) }),
    invite: useMutation({
      mutationFn: () => unwrap(api.POST('/tenants/{id}/invite', path())),
      onSuccess: refresh,
    }),
    resendInvite: useMutation({
      mutationFn: (userId: string) =>
        unwrap(api.POST('/users/{id}/resend-invite', { params: { path: { id: userId } } })),
      onSuccess: refresh,
    }),
    recordConsent: useMutation({
      mutationFn: (body: S['ConsentInput']) => unwrap(api.POST('/tenants/{id}/consents', { ...path(), body })),
      onSuccess: refresh,
    }),
    revokeConsent: useMutation({
      mutationFn: (v: { consentId: string; body: S['ConsentRevokeInput'] }) =>
        unwrap(
          api.POST('/tenants/{id}/consents/{consentId}/revoke', {
            params: { path: { id: toValue(id), consentId: v.consentId } },
            body: v.body,
          }),
        ),
      onSuccess: refresh,
    }),
  }
}
