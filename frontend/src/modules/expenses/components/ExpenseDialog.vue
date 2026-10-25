<script setup lang="ts">
// Диалог «Добавить затрату» / «Изменить затрату» (14 Экраны собственника, раздел 10; 05 разд. 14.2): Объект*,
// Категория*, Сумма*, Дата оплаты*, Месяц отнесения* (по умолчанию месяц оплаты), Описание*, Контрагент, Документ,
// Заявка, «За счёт нанимателя»: нет / удержать из залога / выставить начисление (тогда Договор* и Срок оплаты*).
import dayjs from 'dayjs'
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useContracts } from '@/api/queries/contracts'
import { useExpenseMutations, useRequests, type Expense } from '@/api/queries/maintenance'
import { uploadFile, useProperties } from '@/api/queries/properties'
import type { components } from '@/api/schema'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import { expenseCategoryLabels, options } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { todayIso } from '@/utils/today'

type S = components['schemas']
const props = defineProps<{
  expense?: Expense | null
  propertyId?: string
  requestId?: string
  tenantFault?: boolean
}>()
const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()
const m = useExpenseMutations()
const editing = computed(() => !!props.expense)

const form = reactive({
  propertyId: null as string | null,
  category: 'repair' as S['ExpenseCategory'],
  amountKop: null as number | null,
  paidOn: null as string | null,
  periodMonth: null as string | null,
  description: '',
  counterparty: '',
  fileId: null as string | null,
  fileName: null as string | null,
  maintenanceRequestId: null as string | null,
  recovery: 'none' as S['ExpenseRecovery'],
  contractId: null as string | null,
  chargeDueDate: null as string | null,
})
const amountError = ref<string | null>(null)
const submitted = ref(false)
const uploading = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
/** Месяц отнесения меняли вручную — дата оплаты его больше не переносит. */
const monthTouched = ref(false)

watch(
  visible,
  (v) => {
    if (!v) return
    const e = props.expense
    Object.assign(form, {
      propertyId: e?.property.id ?? props.propertyId ?? null,
      category: e?.category ?? 'repair',
      amountKop: e?.amountKop ?? null,
      paidOn: e?.paidOn ?? todayIso(),
      periodMonth: e?.periodMonth ?? dayjs(todayIso()).startOf('month').format('YYYY-MM-DD'),
      description: e?.description ?? '',
      counterparty: e?.counterparty ?? '',
      fileId: e?.file?.id ?? null,
      fileName: e?.file?.name ?? null,
      maintenanceRequestId: e?.maintenanceRequest?.id ?? props.requestId ?? null,
      recovery: e?.recovery ?? (props.tenantFault ? 'deposit' : 'none'),
      contractId: null,
      chargeDueDate: dayjs(todayIso()).add(10, 'day').format('YYYY-MM-DD'),
    })
    monthTouched.value = !!e
    amountError.value = null
    submitted.value = false
    serverErrors.value = {}
    serverError.value = null
  },
  { immediate: true },
)
watch(
  () => form.paidOn,
  (d) => {
    if (d && !monthTouched.value) form.periodMonth = dayjs(d).startOf('month').format('YYYY-MM-DD')
  },
)

const properties = useProperties(() => ({ archived: false, pageSize: 100 }))
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((p) => ({ value: p.id, label: `${p.code} — ${p.address}` })),
)
const requests = useRequests(
  () => ({ propertyId: form.propertyId ?? undefined, pageSize: 100 }),
  () => visible.value && !!form.propertyId,
)
const requestOptions = computed(() =>
  (requests.data.value?.items ?? []).map((r) => ({ value: r.id, label: `${r.number} — ${r.title}` })),
)
const needsContract = computed(() => !editing.value && form.recovery !== 'none')
const contracts = useContracts(() => ({
  propertyId: form.propertyId ?? undefined,
  status: ['active', 'extended', 'terminated', 'finished'],
  pageSize: 100,
}))
// Только договоры выбранного объекта: пока объект не подставлен (или ответ ещё прежний — keepPreviousData),
// в списке могут быть договоры других объектов.
const propertyContracts = computed(() =>
  form.propertyId ? (contracts.data.value?.items ?? []).filter((c) => c.property.id === form.propertyId) : [],
)
const contractOptions = computed(() =>
  propertyContracts.value.map((c) => ({
    value: c.id,
    label: `№ ${c.number} — ${c.tenant.displayName}`,
  })),
)
// Договор по умолчанию — действующий или последний по объекту (05 разд. 14.2).
watch([contractOptions, needsContract], () => {
  if (!needsContract.value) return
  if (form.contractId && contractOptions.value.some((o) => o.value === form.contractId)) return
  const items = propertyContracts.value
  const active = items.find((c) => c.status === 'active' || c.status === 'extended')
  form.contractId = (active ?? items[0])?.id ?? null
})
watch(
  () => form.propertyId,
  (_, old) => {
    if (old !== undefined && old !== null) {
      form.contractId = null
      form.maintenanceRequestId = null
    }
  },
)

const recoveryOptions = [
  { value: 'none', label: 'Нет' },
  { value: 'deposit', label: 'Удержать из залога' },
  { value: 'charge', label: 'Выставить начисление' },
]

async function pickFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  uploading.value = true
  serverErrors.value = {}
  try {
    const dto = await uploadFile(f, 'expense_doc')
    form.fileId = dto.id
    form.fileName = dto.name
  } catch (err) {
    serverErrors.value = { fileId: parseProblem(err).message }
  } finally {
    uploading.value = false
    ;(e.target as HTMLInputElement).value = ''
  }
}

const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!form.propertyId) e.propertyId = 'Выберите объект'
  if (amountError.value) e.amountKop = amountError.value
  else if (!form.amountKop || form.amountKop <= 0) e.amountKop = 'Сумма должна быть больше нуля'
  if (!form.paidOn) e.paidOn = 'Укажите дату оплаты'
  if (!form.periodMonth) e.periodMonth = 'Укажите месяц отнесения'
  if (!form.description.trim()) e.description = 'Укажите описание'
  if (needsContract.value && !form.contractId) e.contractId = 'Выберите договор'
  if (!editing.value && form.recovery === 'charge' && !form.chargeDueDate) e.chargeDueDate = 'Укажите срок оплаты'
  return { ...e, ...serverErrors.value }
})
const err = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(errors.value).length) return
  const body: S['ExpenseUpdateInput'] = {
    propertyId: form.propertyId!,
    category: form.category,
    amountKop: form.amountKop!,
    paidOn: form.paidOn!,
    periodMonth: form.periodMonth!,
    description: form.description.trim(),
    counterparty: form.counterparty.trim() || null,
    fileId: form.fileId,
    maintenanceRequestId: form.maintenanceRequestId,
  }
  try {
    if (props.expense) {
      await m.update.mutateAsync({ id: props.expense.id, body })
      toast.add({ severity: 'success', summary: 'Затрата изменена', life: 3000 })
    } else {
      await m.create.mutateAsync({
        ...body,
        recovery: form.recovery,
        contractId: form.recovery === 'none' ? null : form.contractId,
        chargeDueDate: form.recovery === 'charge' ? form.chargeDueDate : null,
      })
      const summary = {
        none: 'Затрата добавлена',
        deposit: 'Затрата добавлена, сумма удержана из залога',
        charge: 'Затрата добавлена, нанимателю выставлено начисление',
      }[form.recovery]
      toast.add({ severity: 'success', summary, life: 4000 })
    }
    visible.value = false
  } catch (e) {
    const p = parseProblem(e)
    serverErrors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    :header="editing ? 'Изменить затрату' : 'Добавить затрату'"
    :style="{ width: '640px' }"
    :breakpoints="{ '680px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="save">
      <div class="form-grid">
        <div class="field field--wide">
          <label for="ex-property">Объект *</label>
          <Select
            v-model="form.propertyId"
            input-id="ex-property"
            :options="propertyOptions"
            option-label="label"
            option-value="value"
            filter
            :disabled="!!propertyId && !editing"
            :invalid="!!err('propertyId')"
            fluid
          />
          <small v-if="err('propertyId')" class="error">{{ err('propertyId') }}</small>
        </div>
        <div class="field">
          <label for="ex-category">Категория *</label>
          <Select
            v-model="form.category"
            input-id="ex-category"
            :options="options(expenseCategoryLabels)"
            option-label="label"
            option-value="value"
            fluid
          />
        </div>
        <div class="field">
          <label for="ex-amount">Сумма *</label>
          <MoneyInput
            v-model="form.amountKop"
            input-id="ex-amount"
            :invalid="!!err('amountKop')"
            @error="(e) => (amountError = e)"
          />
          <small v-if="err('amountKop')" class="error">{{ err('amountKop') }}</small>
        </div>
        <div class="field">
          <label for="ex-paid">Дата оплаты *</label>
          <DateInput v-model="form.paidOn" input-id="ex-paid" :invalid="!!err('paidOn')" />
          <small v-if="err('paidOn')" class="error">{{ err('paidOn') }}</small>
        </div>
        <div class="field">
          <label for="ex-month">Месяц отнесения *</label>
          <DateInput
            v-model="form.periodMonth"
            input-id="ex-month"
            :invalid="!!err('periodMonth')"
            @blur="monthTouched = true"
          />
          <small class="hint">Учитывается месяц выбранной даты</small>
          <small v-if="err('periodMonth')" class="error">{{ err('periodMonth') }}</small>
        </div>
        <div class="field field--wide">
          <label for="ex-desc">Описание *</label>
          <InputText id="ex-desc" v-model="form.description" maxlength="1000" :invalid="!!err('description')" fluid />
          <small v-if="err('description')" class="error">{{ err('description') }}</small>
        </div>
        <div class="field">
          <label for="ex-counterparty">Контрагент</label>
          <InputText id="ex-counterparty" v-model="form.counterparty" maxlength="300" fluid />
        </div>
        <div class="field">
          <label for="ex-request">Заявка</label>
          <Select
            v-model="form.maintenanceRequestId"
            input-id="ex-request"
            :options="requestOptions"
            option-label="label"
            option-value="value"
            filter
            show-clear
            :disabled="!form.propertyId"
            :invalid="!!err('maintenanceRequestId')"
            fluid
          />
          <small v-if="err('maintenanceRequestId')" class="error">{{ err('maintenanceRequestId') }}</small>
        </div>
        <div class="field field--wide">
          <span class="label">Документ</span>
          <div class="file">
            <a v-if="form.fileId" :href="`/api/v1/files/${form.fileId}`" target="_blank" rel="noopener">{{
              form.fileName ?? 'Документ'
            }}</a>
            <span v-else class="hint">Чек, акт подрядчика (PDF или изображение)</span>
            <label class="p-button p-button-outlined p-button-sm upload">
              <i class="pi pi-paperclip" aria-hidden="true" />
              {{ uploading ? 'Загрузка…' : form.fileId ? 'Заменить' : 'Прикрепить' }}
              <input type="file" accept=".pdf,image/jpeg,image/png,image/webp" hidden @change="pickFile" />
            </label>
            <Button v-if="form.fileId" label="Убрать" text size="small" @click="form.fileId = null" />
          </div>
          <small v-if="err('fileId')" class="error">{{ err('fileId') }}</small>
        </div>
      </div>

      <div v-if="!editing" class="field">
        <label id="ex-recovery">За счёт нанимателя</label>
        <SelectButton
          v-model="form.recovery"
          :options="recoveryOptions"
          option-label="label"
          option-value="value"
          :allow-empty="false"
          aria-labelledby="ex-recovery"
        />
      </div>
      <p v-else-if="expense && expense.recovery !== 'none'" class="hint">
        Затрата возмещена нанимателем — сумму и объект изменить нельзя.
      </p>
      <div v-if="needsContract" class="form-grid">
        <div class="field">
          <label for="ex-contract">Договор *</label>
          <Select
            v-model="form.contractId"
            input-id="ex-contract"
            :options="contractOptions"
            option-label="label"
            option-value="value"
            :invalid="!!err('contractId')"
            fluid
          />
          <small v-if="err('contractId')" class="error">{{ err('contractId') }}</small>
        </div>
        <div v-if="form.recovery === 'charge'" class="field">
          <label for="ex-due">Срок оплаты *</label>
          <DateInput v-model="form.chargeDueDate" input-id="ex-due" :invalid="!!err('chargeDueDate')" />
          <small v-if="err('chargeDueDate')" class="error">{{ err('chargeDueDate') }}</small>
        </div>
      </div>

      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button
          type="submit"
          :label="editing ? 'Сохранить' : 'Добавить'"
          :loading="m.create.isPending.value || m.update.isPending.value"
          :disabled="uploading"
        />
      </div>
    </form>
  </Dialog>
</template>

<style scoped>
.file {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}
.upload {
  cursor: pointer;
  gap: 0.35rem;
}
.label {
  font-weight: 600;
}
</style>
