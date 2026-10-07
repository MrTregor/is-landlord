<script setup lang="ts">
// Настройки → Организация (14 Экраны собственника, раздел 16.1).
import { ref, watch } from 'vue'
import { useForm } from 'vee-validate'
import { useToast } from 'primevue/usetoast'
import { toInput, useOrganizationSettings, useSaveOrganizationSettings } from '@/api/queries/settings'
import { parseProblem } from '@/utils/problem'
import { toTypedSchema } from '@/utils/zodSchema'
import { organizationSchema, type OrganizationForm } from '../schemas'

const settings = useOrganizationSettings()
const save = useSaveOrganizationSettings()
const toast = useToast()
const serverError = ref<string | null>(null)

const {
  values,
  errors,
  handleSubmit,
  resetForm,
  setErrors,
  setFieldValue,
  validateField,
  setFieldTouched,
  isFieldTouched,
  submitCount,
  meta,
} = useForm({
  validationSchema: toTypedSchema(organizationSchema),
})

const fields: { key: keyof OrganizationForm; label: string; required?: boolean; wide?: boolean; hint?: string }[] = [
  {
    key: 'fullName',
    label: 'Полное наименование',
    required: true,
    wide: true,
    hint: 'Индивидуальный предприниматель Фамилия Имя Отчество',
  },
  { key: 'shortName', label: 'Краткое наименование', required: true, hint: 'ИП Фамилия И.О.' },
  { key: 'inn', label: 'ИНН', required: true },
  { key: 'ogrnip', label: 'ОГРНИП' },
  { key: 'address', label: 'Адрес', required: true, wide: true },
  {
    key: 'documentCity',
    label: 'Город для документов',
    required: true,
    hint: 'Место составления договоров: «г. Руза»',
  },
  { key: 'phone', label: 'Телефон' },
  { key: 'email', label: 'E-mail', required: true },
  { key: 'signatoryName', label: 'Подписант', required: true, hint: '«А.В. Гордейчик»' },
  { key: 'bankName', label: 'Банк', wide: true },
  { key: 'bankBik', label: 'БИК' },
  { key: 'bankAccount', label: 'Расчётный счёт' },
  { key: 'bankCorrAccount', label: 'Корреспондентский счёт' },
]

watch(
  () => settings.data.value,
  (s) => {
    if (!s) return
    resetForm({
      values: {
        fullName: s.fullName,
        shortName: s.shortName,
        inn: s.inn,
        ogrnip: s.ogrnip ?? '',
        address: s.address,
        documentCity: s.documentCity,
        phone: s.phone ?? '',
        email: s.email,
        signatoryName: s.signatoryName,
        bankName: s.bankName ?? '',
        bankBik: s.bankBik ?? '',
        bankAccount: s.bankAccount ?? '',
        bankCorrAccount: s.bankCorrAccount ?? '',
      },
    })
  },
  { immediate: true },
)

// Ошибка поля видна после первого ухода с поля или попытки отправки (13 Фронтенд, раздел 3).
const shown = (key: keyof OrganizationForm) =>
  submitCount.value > 0 || isFieldTouched(key) ? errors.value[key] : undefined
function onBlur(key: keyof OrganizationForm) {
  setFieldTouched(key, true)
  void validateField(key)
}

const orNull = (v: string) => (v.trim() === '' ? null : v.trim())

const onSubmit = handleSubmit(async (v) => {
  if (!settings.data.value) return
  serverError.value = null
  try {
    await save.mutateAsync({
      ...toInput(settings.data.value),
      ...v,
      ogrnip: orNull(v.ogrnip),
      phone: orNull(v.phone),
      bankName: orNull(v.bankName),
      bankBik: orNull(v.bankBik),
      bankAccount: orNull(v.bankAccount),
      bankCorrAccount: orNull(v.bankCorrAccount),
    })
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
  } catch (err) {
    const p = parseProblem(err)
    setErrors(p.fieldErrors)
    serverError.value = p.message
  }
})
</script>

<template>
  <Skeleton v-if="settings.isPending.value" height="20rem" />
  <form v-else class="form" novalidate @submit="onSubmit">
    <div class="form-grid">
      <div v-for="f in fields" :key="f.key" class="field" :class="{ 'field--wide': f.wide }">
        <label :for="`org-${f.key}`">{{ f.label }}<span v-if="f.required"> *</span></label>
        <InputText
          :id="`org-${f.key}`"
          :model-value="values[f.key]"
          :invalid="!!shown(f.key)"
          fluid
          @update:model-value="(v) => setFieldValue(f.key, v ?? '')"
          @blur="onBlur(f.key)"
        />
        <small v-if="shown(f.key)" class="error">{{ shown(f.key) }}</small>
        <small v-else-if="f.hint" class="hint">{{ f.hint }}</small>
      </div>
    </div>
    <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
    <div class="actions">
      <Button
        type="submit"
        label="Сохранить"
        icon="pi pi-check"
        :loading="save.isPending.value"
        :disabled="!meta.dirty"
      />
    </div>
  </form>
</template>
