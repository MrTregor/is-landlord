// Письма (FR-10): шаблоны писем и журнал отправки.
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { api, unwrap } from '../client'
import type { components, operations } from '../schema'

type S = components['schemas']
export type EmailTemplateCode = S['EmailTemplateCode']
export type EmailTemplateItem = S['EmailTemplateListItem']
export type EmailTemplate = S['EmailTemplateDTO']
export type EmailLogItem = S['EmailLogDTO']
export type EmailLogQuery = NonNullable<operations['listEmailLog']['parameters']['query']>

export function useEmailTemplates() {
  return useQuery({
    queryKey: ['email-templates'],
    queryFn: () => unwrap(api.GET('/settings/email-templates')),
  })
}

export function useEmailTemplate(code: MaybeRefOrGetter<EmailTemplateCode>) {
  return useQuery({
    queryKey: ['email-templates', code],
    queryFn: () => unwrap(api.GET('/settings/email-templates/{code}', { params: { path: { code: toValue(code) } } })),
  })
}

export function useEmailTemplateMutations(code: MaybeRefOrGetter<EmailTemplateCode>) {
  const qc = useQueryClient()
  const path = () => ({ params: { path: { code: toValue(code) } } })
  const done = (d: EmailTemplate) => {
    qc.setQueryData(['email-templates', toValue(code)], d)
    void qc.invalidateQueries({ queryKey: ['email-templates'], exact: true })
  }
  return {
    save: useMutation({
      mutationFn: (body: S['EmailTemplateInput']) =>
        unwrap(api.PUT('/settings/email-templates/{code}', { ...path(), body })),
      onSuccess: done,
    }),
    reset: useMutation({
      mutationFn: () => unwrap(api.POST('/settings/email-templates/{code}/reset', path())),
      onSuccess: done,
    }),
    preview: useMutation({
      mutationFn: (body: S['EmailTemplatePreviewRequest']) =>
        unwrap(api.POST('/settings/email-templates/{code}/preview', { ...path(), body })),
    }),
  }
}

export function useEmailLog(params: MaybeRefOrGetter<EmailLogQuery>) {
  return useQuery({
    queryKey: ['email-log', params],
    queryFn: () => unwrap(api.GET('/email-log', { params: { query: toValue(params) } })),
    placeholderData: keepPreviousData,
  })
}

export function useResendEmail() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => unwrap(api.POST('/email-log/{id}/resend', { params: { path: { id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['email-log'] }),
  })
}
