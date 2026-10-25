<script setup lang="ts">
// Наниматель: создание и изменение (14 Экраны собственника, раздел 3.2).
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import { useToast } from 'primevue/usetoast'
import { api, unwrap } from '@/api/client'
import { uploadFile } from '@/api/queries/properties'
import { useOrganizationSettings } from '@/api/queries/settings'
import { useTenant } from '@/api/queries/tenants'
import DateInput from '@/components/DateInput.vue'
import PageHeader from '@/components/PageHeader.vue'
import { docTypeLabels, options } from '@/utils/labels'
import { normalizePhone } from '@/utils/phone'
import { parseProblem } from '@/utils/problem'
import { today, todayIso } from '@/utils/today'
import { emptyTenantForm, tenantSchema, type TenantForm } from '../schemas'

// embedded — форма в диалоге «Новый наниматель» мастера договора: без заголовка, после сохранения — событие saved.
const props = defineProps<{ embedded?: boolean }>()
const emit = defineEmits<{ saved: [id: string]; cancel: [] }>()
const route = useRoute()
const router = useRouter()
const toast = useToast()
const qc = useQueryClient()
const org = useOrganizationSettings()
const id = computed(() => (props.embedded ? '' : ((route.params.id as string | undefined) ?? '')))
const isEdit = computed(() => !!id.value)
const existing = useTenant(id)

const form = reactive<TenantForm>(emptyTenantForm())
const touched = reactive(new Set<string>())
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
const saving = ref(false)

// Документ на изменении: сохранённое значение не показывается; «Очистить» удаляет его.
const docFields = ['docNumber', 'docIssuedBy', 'docDivisionCode', 'registrationAddress'] as const
const cleared = reactive(new Set<string>())

watch(
  () => existing.data.value,
  (t) => {
    if (!t || !isEdit.value) return
    Object.assign(form, emptyTenantForm(t.kind), {
      lastName: t.lastName ?? '',
      firstName: t.firstName ?? '',
      middleName: t.middleName ?? '',
      birthDate: t.birthDate,
      docType: t.docType,
      docIssuedOn: t.docIssuedOn,
      orgName: t.orgName ?? '',
      inn: t.inn ?? '',
      kpp: t.kpp ?? '',
      ogrn: t.ogrn ?? '',
      legalAddress: t.legalAddress ?? '',
      signatoryName: t.signatoryName ?? '',
      signatoryPosition: t.signatoryPosition ?? '',
      signatoryBasis: t.signatoryBasis ?? '',
      phone: t.phone ?? '',
      email: t.email ?? '',
      note: t.note ?? '',
    })
  },
  { immediate: true },
)

const clientErrors = computed<Record<string, string>>(() => {
  const r = tenantSchema.safeParse(form)
  const out: Record<string, string> = {}
  if (!r.success) for (const i of r.error.issues) out[String(i.path[0])] ??= i.message
  return out
})
const err = (k: string) =>
  serverErrors.value[k] ?? (submitted.value || touched.has(k) ? clientErrors.value[k] : undefined)
const touch = (k: string) => touched.add(k)

// Отдельное согласие на ПДн (только создание физлица, необязательно).
const consent = reactive({ given: false, givenOn: todayIso() as string | null, file: null as File | null })

const orNull = (v: string) => (v.trim() === '' ? null : v.trim())

async function submit() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(clientErrors.value).length) return
  if (consent.given && !consent.givenOn) {
    serverErrors.value = { consent: 'Укажите дату согласия' }
    return
  }
  saving.value = true
  try {
    const phone = form.phone.trim() ? normalizePhone(form.phone) : null
    const body = {
      kind: form.kind,
      lastName: orNull(form.lastName),
      firstName: orNull(form.firstName),
      middleName: orNull(form.middleName),
      birthDate: form.birthDate,
      docType: form.kind === 'individual' ? form.docType : null,
      docIssuedOn: form.docIssuedOn,
      orgName: orNull(form.orgName),
      inn: orNull(form.inn),
      kpp: orNull(form.kpp),
      ogrn: orNull(form.ogrn),
      legalAddress: orNull(form.legalAddress),
      signatoryName: orNull(form.signatoryName),
      signatoryPosition: orNull(form.signatoryPosition),
      signatoryBasis: orNull(form.signatoryBasis),
      phone,
      email: orNull(form.email),
      note: orNull(form.note),
    } as Record<string, unknown>
    // Поля документа: пусто на изменении — не менять; «Очистить» — "".
    for (const f of docFields) {
      const v = form[f].trim()
      if (v) body[f] = v
      else if (!isEdit.value || cleared.has(f)) body[f] = isEdit.value ? '' : undefined
    }
    let tenantId: string
    if (isEdit.value) {
      const t = await unwrap(api.PUT('/tenants/{id}', { params: { path: { id: id.value } }, body: body as never }))
      tenantId = t.id
    } else {
      if (consent.given && form.kind === 'individual') {
        const fileId = consent.file ? (await uploadFile(consent.file, 'consent_scan')).id : null
        body.consent = { givenOn: consent.givenOn, fileId }
      }
      const t = await unwrap(api.POST('/tenants', { body: body as never }))
      tenantId = t.id
    }
    await qc.invalidateQueries({ queryKey: ['tenants'] })
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
    if (props.embedded) emit('saved', tenantId)
    else await router.push(`/tenants/${tenantId}`)
  } catch (e) {
    const p = parseProblem(e)
    serverErrors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  } finally {
    saving.value = false
  }
}

const kindOptions = [
  { value: 'individual', label: 'Физическое лицо' },
  { value: 'legal', label: 'Юридическое лицо' },
]
const docHint = 'Сохранено — оставьте пустым, чтобы не менять'
const hasSavedDoc = computed(() => isEdit.value && !!existing.data.value?.hasDocument)
</script>

<template>
  <div>
    <PageHeader
      v-if="!embedded"
      :title="isEdit ? `Изменение: ${existing.data.value?.displayName ?? ''}` : 'Новый наниматель'"
    />
    <Skeleton v-if="isEdit && existing.isPending.value" height="24rem" />
    <form v-else class="form card" novalidate @submit.prevent="submit">
      <SelectButton
        v-model="form.kind"
        :options="kindOptions"
        option-label="label"
        option-value="value"
        :allow-empty="false"
        :disabled="isEdit"
        aria-label="Тип нанимателя"
      />

      <template v-if="form.kind === 'individual'">
        <div class="form-grid">
          <div class="field">
            <label for="t-last">Фамилия *</label>
            <InputText
              id="t-last"
              v-model="form.lastName"
              :invalid="!!err('lastName')"
              fluid
              @blur="touch('lastName')"
            />
            <small v-if="err('lastName')" class="error">{{ err('lastName') }}</small>
          </div>
          <div class="field">
            <label for="t-first">Имя *</label>
            <InputText
              id="t-first"
              v-model="form.firstName"
              :invalid="!!err('firstName')"
              fluid
              @blur="touch('firstName')"
            />
            <small v-if="err('firstName')" class="error">{{ err('firstName') }}</small>
          </div>
          <div class="field">
            <label for="t-middle">Отчество</label>
            <InputText id="t-middle" v-model="form.middleName" fluid />
          </div>
          <div class="field">
            <label for="t-birth">Дата рождения</label>
            <DateInput v-model="form.birthDate" input-id="t-birth" :max-date="today()" :invalid="!!err('birthDate')" />
            <small v-if="err('birthDate')" class="error">{{ err('birthDate') }}</small>
          </div>
        </div>

        <fieldset class="group">
          <legend>Документ</legend>
          <p class="hint">Документ и адрес регистрации нужны для договора найма.</p>
          <div class="form-grid">
            <div class="field">
              <label for="t-doctype">Вид</label>
              <Select
                v-model="form.docType"
                input-id="t-doctype"
                :options="options(docTypeLabels)"
                option-label="label"
                option-value="value"
                fluid
              />
            </div>
            <div class="field">
              <label for="t-docnum">Серия и номер</label>
              <InputMask
                v-if="form.docType === 'passport_rf'"
                id="t-docnum"
                v-model="form.docNumber"
                mask="99 99 999999"
                :auto-clear="false"
                :invalid="!!err('docNumber')"
                fluid
                @blur="touch('docNumber')"
              />
              <InputText v-else id="t-docnum" v-model="form.docNumber" :invalid="!!err('docNumber')" fluid />
              <small v-if="err('docNumber')" class="error">{{ err('docNumber') }}</small>
              <small v-else-if="hasSavedDoc && !cleared.has('docNumber')" class="hint">{{ docHint }}</small>
            </div>
            <div class="field field--wide">
              <label for="t-issued">Кем выдан</label>
              <InputText id="t-issued" v-model="form.docIssuedBy" fluid />
              <small v-if="hasSavedDoc && !cleared.has('docIssuedBy')" class="hint">{{ docHint }}</small>
            </div>
            <div class="field">
              <label for="t-issuedon">Дата выдачи</label>
              <DateInput v-model="form.docIssuedOn" input-id="t-issuedon" :max-date="today()" />
            </div>
            <div class="field">
              <label for="t-division">Код подразделения</label>
              <InputMask
                id="t-division"
                v-model="form.docDivisionCode"
                mask="999-999"
                :auto-clear="false"
                :invalid="!!err('docDivisionCode')"
                fluid
                @blur="touch('docDivisionCode')"
              />
              <small v-if="err('docDivisionCode')" class="error">{{ err('docDivisionCode') }}</small>
            </div>
            <div class="field field--wide">
              <label for="t-regaddr">Адрес регистрации</label>
              <InputText id="t-regaddr" v-model="form.registrationAddress" fluid />
              <small v-if="hasSavedDoc && !cleared.has('registrationAddress')" class="hint">{{ docHint }}</small>
            </div>
          </div>
          <div v-if="hasSavedDoc" class="actions">
            <Button
              type="button"
              text
              severity="danger"
              label="Очистить документ и адрес"
              @click="docFields.forEach((f) => cleared.add(f))"
            />
          </div>
          <Message v-if="cleared.size" severity="warn" :closable="false">
            Сохранённые документ и адрес регистрации будут удалены при сохранении.
          </Message>
        </fieldset>
      </template>

      <template v-else>
        <div class="form-grid">
          <div class="field field--wide">
            <label for="t-org">Наименование *</label>
            <InputText
              id="t-org"
              v-model="form.orgName"
              placeholder="ООО «Ромашка»"
              :invalid="!!err('orgName')"
              fluid
              @blur="touch('orgName')"
            />
            <small v-if="err('orgName')" class="error">{{ err('orgName') }}</small>
          </div>
          <div class="field">
            <label for="t-inn">ИНН *</label>
            <InputText
              id="t-inn"
              v-model="form.inn"
              inputmode="numeric"
              maxlength="10"
              :invalid="!!err('inn')"
              fluid
              @blur="touch('inn')"
            />
            <small v-if="err('inn')" class="error">{{ err('inn') }}</small>
          </div>
          <div class="field">
            <label for="t-kpp">КПП</label>
            <InputText
              id="t-kpp"
              v-model="form.kpp"
              inputmode="numeric"
              maxlength="9"
              :invalid="!!err('kpp')"
              fluid
              @blur="touch('kpp')"
            />
            <small v-if="err('kpp')" class="error">{{ err('kpp') }}</small>
          </div>
          <div class="field">
            <label for="t-ogrn">ОГРН</label>
            <InputText
              id="t-ogrn"
              v-model="form.ogrn"
              inputmode="numeric"
              maxlength="15"
              :invalid="!!err('ogrn')"
              fluid
              @blur="touch('ogrn')"
            />
            <small v-if="err('ogrn')" class="error">{{ err('ogrn') }}</small>
          </div>
          <div class="field field--wide">
            <label for="t-legaladdr">Юридический адрес *</label>
            <InputText
              id="t-legaladdr"
              v-model="form.legalAddress"
              :invalid="!!err('legalAddress')"
              fluid
              @blur="touch('legalAddress')"
            />
            <small v-if="err('legalAddress')" class="error">{{ err('legalAddress') }}</small>
          </div>
          <div class="field">
            <label for="t-sign">Подписант (ФИО) *</label>
            <InputText
              id="t-sign"
              v-model="form.signatoryName"
              :invalid="!!err('signatoryName')"
              fluid
              @blur="touch('signatoryName')"
            />
            <small v-if="err('signatoryName')" class="error">{{ err('signatoryName') }}</small>
          </div>
          <div class="field">
            <label for="t-signpos">Должность подписанта *</label>
            <InputText
              id="t-signpos"
              v-model="form.signatoryPosition"
              placeholder="генеральный директор"
              :invalid="!!err('signatoryPosition')"
              fluid
              @blur="touch('signatoryPosition')"
            />
            <small v-if="err('signatoryPosition')" class="error">{{ err('signatoryPosition') }}</small>
          </div>
          <div class="field">
            <label for="t-signbasis">Основание полномочий *</label>
            <InputText
              id="t-signbasis"
              v-model="form.signatoryBasis"
              placeholder="Устава"
              :invalid="!!err('signatoryBasis')"
              fluid
              @blur="touch('signatoryBasis')"
            />
            <small v-if="err('signatoryBasis')" class="error">{{ err('signatoryBasis') }}</small>
          </div>
        </div>
      </template>

      <div class="form-grid">
        <div class="field">
          <label for="t-phone">Телефон</label>
          <InputMask
            id="t-phone"
            v-model="form.phone"
            mask="+7 (999) 999-99-99"
            :auto-clear="false"
            :unmask="false"
            :invalid="!!err('phone')"
            fluid
            @blur="touch('phone')"
          />
          <small v-if="err('phone')" class="error">{{ err('phone') }}</small>
        </div>
        <div class="field">
          <label for="t-email">E-mail</label>
          <InputText
            id="t-email"
            v-model="form.email"
            type="email"
            :invalid="!!err('email')"
            fluid
            @blur="touch('email')"
          />
          <small v-if="err('email')" class="error">{{ err('email') }}</small>
        </div>
        <div class="field field--wide">
          <label for="t-note">Примечание</label>
          <Textarea id="t-note" v-model="form.note" auto-resize rows="2" fluid />
        </div>
      </div>

      <fieldset v-if="!isEdit && form.kind === 'individual'" class="group">
        <legend>Отдельное согласие на ПДн</legend>
        <p class="hint">
          Для договора согласие не нужно. Оно требуется только для цели:
          {{ org.data.value?.pdnConsentPurpose ?? '…' }}
        </p>
        <div class="field field--check">
          <Checkbox v-model="consent.given" input-id="t-consent" binary />
          <label for="t-consent">Получено подписанное согласие</label>
        </div>
        <div v-if="consent.given" class="form-grid">
          <div class="field">
            <label for="t-consent-date">Дата согласия *</label>
            <DateInput
              v-model="consent.givenOn"
              input-id="t-consent-date"
              :max-date="today()"
              :invalid="!!err('consent')"
            />
            <small v-if="err('consent')" class="error">{{ err('consent') }}</small>
          </div>
          <div class="field">
            <label for="t-consent-file">Скан</label>
            <input
              id="t-consent-file"
              type="file"
              accept="application/pdf,image/jpeg,image/png,image/webp"
              @change="(e) => (consent.file = (e.target as HTMLInputElement).files?.[0] ?? null)"
            />
          </div>
        </div>
      </fieldset>

      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="embedded ? emit('cancel') : router.back()" />
        <Button type="submit" label="Сохранить" :loading="saving" />
      </div>
    </form>
  </div>
</template>

<style scoped>
.card {
  padding: 1.5rem;
  background: var(--p-content-background);
  border-radius: var(--p-border-radius-lg);
}
.group {
  border: 1px solid var(--p-content-border-color);
  border-radius: var(--p-border-radius-md);
  padding: 0.75rem 1rem 1rem;
  margin: 0;
}
.group legend {
  padding: 0 0.25rem;
  font-weight: 600;
}
</style>
