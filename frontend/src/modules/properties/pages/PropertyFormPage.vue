<script setup lang="ts">
// Объект: создание и изменение (14 Экраны собственника, раздел 2.2).
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import { useForm } from 'vee-validate'
import { useToast } from 'primevue/usetoast'
import { api, unwrap } from '@/api/client'
import { useProperty } from '@/api/queries/properties'
import { useOrganizationSettings } from '@/api/queries/settings'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import PageHeader from '@/components/PageHeader.vue'
import { fiscalModeLabels, options, propertyKindLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { toTypedSchema } from '@/utils/zodSchema'
import { today, todayIso } from '@/utils/today'
import { areaToApi, areaToText, propertySchema, type PropertyForm } from '../schemas'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const qc = useQueryClient()
const id = computed(() => (route.params.id as string | undefined) ?? '')
const isEdit = computed(() => !!id.value)
const existing = useProperty(id)
const org = useOrganizationSettings()

const {
  values,
  errors,
  handleSubmit,
  setFieldValue,
  setFieldTouched,
  validateField,
  isFieldTouched,
  submitCount,
  resetForm,
  setErrors,
  isSubmitting,
} = useForm<PropertyForm>({
  validationSchema: toTypedSchema(propertySchema),
  initialValues: {
    code: '',
    kind: 'residential',
    address: '',
    areaSqm: '',
    floor: null,
    rooms: null,
    cadastralNumber: '',
    ownershipBasis: '',
    furnished: false,
    description: '',
    expectedRentKop: 0,
    fiscalMode: 'none',
    initialStatus: 'free',
    statusSince: todayIso(),
  },
})

// Ошибка видна после ухода с поля или попытки отправки.
const shown = (k: keyof PropertyForm) => (submitCount.value > 0 || isFieldTouched(k) ? errors.value[k] : undefined)
function touch(k: keyof PropertyForm) {
  setFieldTouched(k, true)
  void validateField(k)
}
function setV<K extends keyof PropertyForm>(k: K, v: PropertyForm[K]) {
  setFieldValue(k, v as never)
}

watch(
  () => existing.data.value,
  (p) => {
    if (!p || !isEdit.value) return
    resetForm({
      values: {
        code: p.code,
        kind: p.kind,
        address: p.address,
        areaSqm: areaToText(p.areaSqm),
        floor: p.floor,
        rooms: p.rooms,
        cadastralNumber: p.cadastralNumber ?? '',
        ownershipBasis: p.ownershipBasis ?? '',
        furnished: p.furnished,
        description: p.description ?? '',
        expectedRentKop: p.expectedRentKop,
        fiscalMode: p.fiscalMode,
        initialStatus: 'free',
        statusSince: todayIso(),
      },
    })
  },
  { immediate: true },
)

// Новый объект: режим чека по умолчанию — из настроек по виду объекта.
watch(
  [() => values.kind, () => org.data.value],
  ([kind, s]) => {
    if (isEdit.value || !s) return
    setFieldValue(
      'fiscalMode',
      kind === 'residential' ? s.defaultFiscalModeResidential : s.defaultFiscalModeNonResidential,
    )
    if (kind !== 'residential') setFieldValue('rooms', null)
  },
  { immediate: true },
)

const moneyError = ref<string | null>(null)
const serverError = ref<string | null>(null)
const orNull = (v: string) => (v.trim() === '' ? null : v.trim())

const onSubmit = handleSubmit(async (v) => {
  if (moneyError.value) return
  serverError.value = null
  const body = {
    code: v.code,
    kind: v.kind,
    address: v.address,
    areaSqm: areaToApi(v.areaSqm),
    floor: v.floor,
    rooms: v.kind === 'residential' ? v.rooms : null,
    cadastralNumber: orNull(v.cadastralNumber),
    ownershipBasis: orNull(v.ownershipBasis),
    furnished: v.furnished,
    description: orNull(v.description),
    expectedRentKop: v.expectedRentKop,
    fiscalMode: v.fiscalMode,
  }
  try {
    const p = isEdit.value
      ? await unwrap(api.PUT('/properties/{id}', { params: { path: { id: id.value } }, body }))
      : await unwrap(
          api.POST('/properties', { body: { ...body, initialStatus: v.initialStatus, statusSince: v.statusSince } }),
        )
    await qc.invalidateQueries({ queryKey: ['properties'] })
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
    await router.push(`/properties/${p.id}`)
  } catch (err) {
    const p = parseProblem(err)
    setErrors(p.fieldErrors)
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
})
</script>

<template>
  <div>
    <PageHeader :title="isEdit ? `Изменение объекта ${existing.data.value?.code ?? ''}` : 'Новый объект'" />
    <Skeleton v-if="isEdit && existing.isPending.value" height="24rem" />
    <form v-else class="form card" novalidate @submit="onSubmit">
      <div class="form-grid">
        <div class="field">
          <label for="p-code">Короткое название *</label>
          <InputText
            id="p-code"
            :model-value="values.code"
            placeholder="Студия 1"
            :invalid="!!shown('code')"
            fluid
            @update:model-value="(x) => setV('code', x ?? '')"
            @blur="touch('code')"
          />
          <small v-if="shown('code')" class="error">{{ shown('code') }}</small>
        </div>
        <div class="field">
          <span id="p-kind-label">Вид *</span>
          <SelectButton
            :model-value="values.kind"
            :options="options(propertyKindLabels)"
            option-label="label"
            option-value="value"
            :allow-empty="false"
            aria-labelledby="p-kind-label"
            @update:model-value="(x) => setV('kind', x)"
          />
        </div>
        <div class="field field--wide">
          <label for="p-address">Адрес *</label>
          <Textarea
            id="p-address"
            :model-value="values.address"
            auto-resize
            rows="2"
            placeholder="с номером квартиры или помещения"
            :invalid="!!shown('address')"
            fluid
            @update:model-value="(x) => setV('address', x ?? '')"
            @blur="touch('address')"
          />
          <small v-if="shown('address')" class="error">{{ shown('address') }}</small>
        </div>
        <div class="field">
          <label for="p-area">Площадь, м² *</label>
          <InputText
            id="p-area"
            :model-value="values.areaSqm"
            inputmode="decimal"
            :invalid="!!shown('areaSqm')"
            fluid
            @update:model-value="(x) => setV('areaSqm', x ?? '')"
            @blur="touch('areaSqm')"
          />
          <small v-if="shown('areaSqm')" class="error">{{ shown('areaSqm') }}</small>
        </div>
        <div class="field">
          <label for="p-floor">Этаж</label>
          <InputNumber
            input-id="p-floor"
            :model-value="values.floor"
            :min="-5"
            :max="200"
            :use-grouping="false"
            :invalid="!!shown('floor')"
            fluid
            @update:model-value="(x) => setV('floor', x ?? null)"
          />
          <small v-if="shown('floor')" class="error">{{ shown('floor') }}</small>
        </div>
        <div v-if="values.kind === 'residential'" class="field">
          <label for="p-rooms">Комнат</label>
          <InputNumber
            input-id="p-rooms"
            :model-value="values.rooms"
            :min="0"
            :max="20"
            :invalid="!!shown('rooms')"
            fluid
            @update:model-value="(x) => setV('rooms', x ?? null)"
          />
          <small class="hint">0 — студия</small>
        </div>
        <div class="field">
          <label for="p-cad">Кадастровый номер</label>
          <InputText
            id="p-cad"
            :model-value="values.cadastralNumber"
            placeholder="50:19:0010101:123"
            :invalid="!!shown('cadastralNumber')"
            fluid
            @update:model-value="(x) => setV('cadastralNumber', x ?? '')"
            @blur="touch('cadastralNumber')"
          />
          <small v-if="shown('cadastralNumber')" class="error">{{ shown('cadastralNumber') }}</small>
        </div>
        <div class="field field--wide">
          <label for="p-own">Основание права собственности</label>
          <InputText
            id="p-own"
            :model-value="values.ownershipBasis"
            placeholder="Выписка из ЕГРН от …"
            fluid
            @update:model-value="(x) => setV('ownershipBasis', x ?? '')"
          />
          <small class="hint">Подставляется в договор</small>
        </div>
        <div class="field field--check">
          <ToggleSwitch
            input-id="p-furn"
            :model-value="values.furnished"
            @update:model-value="(x) => setV('furnished', x)"
          />
          <label for="p-furn">С мебелью</label>
        </div>
        <div class="field">
          <label for="p-rent">Ожидаемая плата в месяц</label>
          <MoneyInput
            input-id="p-rent"
            :model-value="values.expectedRentKop"
            :invalid="!!moneyError"
            @update:model-value="(x) => setV('expectedRentKop', x ?? 0)"
            @error="(m) => (moneyError = m)"
          />
          <small v-if="moneyError" class="error">{{ moneyError }}</small>
          <small v-else class="hint">Для расчёта недополученного дохода при простое</small>
        </div>
        <div class="field">
          <label for="p-fiscal">Режим чека *</label>
          <Select
            input-id="p-fiscal"
            :model-value="values.fiscalMode"
            :options="options(fiscalModeLabels)"
            option-label="label"
            option-value="value"
            fluid
            @update:model-value="(x) => setV('fiscalMode', x)"
          />
          <small v-if="values.kind === 'non_residential' && values.fiscalMode === 'none'" class="error">
            По нежилым объектам освобождение от применения ККТ не действует
          </small>
        </div>
        <div class="field field--wide">
          <label for="p-desc">Описание</label>
          <Textarea
            id="p-desc"
            :model-value="values.description"
            auto-resize
            rows="2"
            fluid
            @update:model-value="(x) => setV('description', x ?? '')"
          />
        </div>
        <template v-if="!isEdit">
          <div class="field">
            <span id="p-status-label">Начальный статус *</span>
            <SelectButton
              :model-value="values.initialStatus"
              :options="[
                { value: 'free', label: 'Свободен' },
                { value: 'repair', label: 'На ремонте' },
              ]"
              option-label="label"
              option-value="value"
              :allow-empty="false"
              aria-labelledby="p-status-label"
              @update:model-value="(x) => setV('initialStatus', x)"
            />
          </div>
          <div class="field">
            <label for="p-since">Статус с *</label>
            <DateInput
              input-id="p-since"
              :model-value="values.statusSince"
              :max-date="today()"
              :invalid="!!shown('statusSince')"
              @update:model-value="(x) => setV('statusSince', x ?? '')"
            />
            <small v-if="shown('statusSince')" class="error">{{ shown('statusSince') }}</small>
            <small v-else class="hint">Можно указать дату в прошлом для переноса истории</small>
          </div>
        </template>
      </div>
      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="router.back()" />
        <Button type="submit" label="Сохранить" :loading="isSubmitting" />
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
</style>
