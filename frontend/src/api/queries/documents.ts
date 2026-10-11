// Документы и шаблоны документов (FR-04, 09 Документы и шаблоны).
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components } from '../schema'

type S = components['schemas']
export type DocumentDTO = S['DocumentDTO']
export type DocumentKind = S['DocumentKind']
export type TemplateDetail = S['DocumentTemplateDetail']
export type TemplateVersion = S['DocumentTemplateVersion']
export type TemplateVariable = S['TemplateVariable']

export function useContractDocuments(contractId: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: ['contracts', contractId, 'documents'],
    queryFn: () => unwrap(api.GET('/contracts/{id}/documents', { params: { path: { id: toValue(contractId) } } })),
    enabled: () => !!toValue(contractId),
  })
}

/** Формирование документов; после формирования обновляются карточки договора и нанимателя. */
export function useDocumentMutations() {
  const qc = useQueryClient()
  const refresh = () =>
    Promise.all([qc.invalidateQueries({ queryKey: ['contracts'] }), qc.invalidateQueries({ queryKey: ['tenants'] })])
  const path = (id: string) => ({ params: { path: { id } } })
  return {
    contract: useMutation({
      mutationFn: (contractId: string) => unwrap(api.POST('/contracts/{id}/documents/contract', path(contractId))),
      onSuccess: refresh,
    }),
    reconciliation: useMutation({
      mutationFn: (v: { contractId: string; from: string; to: string }) =>
        unwrap(
          api.POST('/contracts/{id}/documents/reconciliation-act', {
            ...path(v.contractId),
            body: { from: v.from, to: v.to },
          }),
        ),
      onSuccess: refresh,
    }),
    act: useMutation({
      mutationFn: (actId: string) => unwrap(api.POST('/acts/{id}/document', path(actId))),
      onSuccess: refresh,
    }),
    amendment: useMutation({
      mutationFn: (amendmentId: string) => unwrap(api.POST('/amendments/{id}/document', path(amendmentId))),
      onSuccess: refresh,
    }),
    consentForm: useMutation({
      mutationFn: (tenantId: string) => unwrap(api.POST('/tenants/{id}/consent-form', path(tenantId))),
      onSuccess: refresh,
    }),
    visibility: useMutation({
      mutationFn: (v: { id: string; visibleToTenant: boolean }) =>
        unwrap(api.PATCH('/documents/{id}', { ...path(v.id), body: { visibleToTenant: v.visibleToTenant } })),
      onSuccess: refresh,
    }),
    signedScan: useMutation({
      mutationFn: (v: { id: string; fileId: string }) =>
        unwrap(api.POST('/documents/{id}/signed-scan', { ...path(v.id), body: { fileId: v.fileId } })),
      onSuccess: refresh,
    }),
  }
}

/** Адрес PDF документа собственника. */
export const documentPdfUrl = (id: string) => `/api/v1/documents/${id}/pdf`

export function useDocumentTemplates() {
  return useQuery({
    queryKey: ['document-templates'],
    queryFn: () => unwrap(api.GET('/settings/document-templates')),
  })
}

export function useDocumentTemplate(kind: MaybeRefOrGetter<DocumentKind>) {
  return useQuery({
    queryKey: ['document-templates', kind],
    queryFn: () =>
      unwrap(api.GET('/settings/document-templates/{kind}', { params: { path: { kind: toValue(kind) } } })),
  })
}

export const fetchTemplateVersion = (kind: DocumentKind, id: string) =>
  unwrap(api.GET('/settings/document-templates/{kind}/versions/{id}', { params: { path: { kind, id } } }))

export function useTemplateVariables() {
  return useQuery({
    queryKey: ['template-variables'],
    queryFn: () => unwrap(api.GET('/settings/template-variables')),
    staleTime: Infinity,
  })
}

export function useTemplateMutations(kind: MaybeRefOrGetter<DocumentKind>) {
  const qc = useQueryClient()
  const path = () => ({ params: { path: { kind: toValue(kind) } } })
  const refresh = () => qc.invalidateQueries({ queryKey: ['document-templates'] })
  return {
    save: useMutation({
      mutationFn: (body: S['DocumentTemplateInput']) =>
        unwrap(api.PUT('/settings/document-templates/{kind}', { ...path(), body })),
      onSuccess: refresh,
    }),
    reset: useMutation({
      mutationFn: () => unwrap(api.POST('/settings/document-templates/{kind}/reset', path())),
      onSuccess: refresh,
    }),
    preview: useMutation({
      mutationFn: (bodyHtml: string) =>
        unwrap(
          api.POST('/settings/document-templates/{kind}/preview', { ...path(), body: { bodyHtml }, parseAs: 'blob' }),
        ) as Promise<Blob>,
    }),
  }
}
