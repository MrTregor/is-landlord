<script setup lang="ts">
// Диалог «Зарегистрировать платёж» (14 Экраны собственника, раздел 6.2): договор, назначение, сумма, дата поступления,
// способ, выбор начислений, комментарий; под формой — предпросмотр распределения (окончательно считает сервер).
import dayjs from 'dayjs'
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useCharges } from '@/api/queries/charges'
import { useContracts } from '@/api/queries/contracts'
import { usePaymentMutations, type PaymentDetail } from '@/api/queries/payments'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import { formatDate } from '@/utils/date'
import { manualPaymentMethodLabels, options, paymentPurposeFormLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { previewAllocation } from '../allocation'

const props = defineProps<{ contractId?: string; purpose?: 'charges' | 'deposit' }>()
const visible = defineModel<boolean>('visible', { required: true })
const emit = defineEmits<{ created: [payment: PaymentDetail] }>()
const toast = useToast()
const m = usePaymentMutations()

const contracts = useContracts(() => ({ status: ['active', 'extended', 'terminated', 'finished'], pageSize: 100 }))
const contractOptions = computed(() =>
  (contracts.data.value?.items ?? []).map((c) => ({
    value: c.id,
    label: `№ ${c.number} — ${c.property.code}, ${c.tenant.displayName}`,
  })),
)

const form = reactive({
  contractId: null as string | null,
  purpose: 'charges' as 'charges' | 'deposit',
  amountKop: null as number | null,
  receivedOn: null as string | null,
  method: 'bank_transfer' as 'bank_transfer' | 'cash',
  chargeIds: [] as string[],
  comment: '',
})
const pickCharges = ref(false)
const amountError = ref<string | null>(null)
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
const today = () => dayjs().format('YYYY-MM-DD')

watch(visible, (v) => {
  if (!v) return
  Object.assign(form, {
    contractId: props.contractId ?? null,
    purpose: props.purpose ?? 'charges',
    amountKop: null,
    receivedOn: today(),
    method: 'bank_transfer',
    chargeIds: [],
    comment: '',
  })
  pickCharges.value = false
  amountError.value = null
  submitted.value = false
  serverErrors.value = {}
  serverError.value = null
})
watch(
  () => form.contractId,
  () => (form.chargeIds = []),
)

const charges = useCharges(
  () => ({
    contractId: form.contractId ?? undefined,
    status: ['open', 'partially_paid'],
    sort: 'dueDate',
    pageSize: 100,
  }),
  () => visible.value && !!form.contractId && form.purpose === 'charges',
)
const openCharges = computed(() =>
  form.contractId && form.purpose === 'charges'
    ? (charges.data.value?.items ?? []).filter((c) => !(c.kind === 'penalty' && c.penaltyState === 'accruing'))
    : [],
)
const selected = computed({
  get: () => openCharges.value.filter((c) => form.chargeIds.includes(c.id)),
  set: (rows) => (form.chargeIds = rows.map((r) => r.id)),
})
const preview = computed(() =>
  form.purpose === 'charges' && form.amountKop && form.amountKop > 0
    ? previewAllocation(form.amountKop, openCharges.value, form.chargeIds)
    : null,
)

const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!form.contractId) e.contractId = 'Выберите договор'
  if (amountError.value) e.amountKop = amountError.value
  else if (!form.amountKop || form.amountKop <= 0) e.amountKop = 'Сумма должна быть больше нуля'
  if (!form.receivedOn) e.receivedOn = 'Укажите дату поступления'
  else if (form.receivedOn > today()) e.receivedOn = 'Дата поступления не может быть позже сегодняшней'
  return { ...e, ...serverErrors.value }
})
const err = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(errors.value).length) return
  try {
    const p = await m.create.mutateAsync({
      contractId: form.contractId!,
      purpose: form.purpose,
      amountKop: form.amountKop!,
      receivedOn: form.receivedOn!,
      method: form.method,
      chargeIds: form.purpose === 'charges' ? form.chargeIds : [],
      comment: form.comment.trim() || null,
    })
    visible.value = false
    toast.add({ severity: 'success', summary: `Платёж зарегистрирован, квитанция ${p.receiptNumber}`, life: 4000 })
    emit('created', p)
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
    header="Зарегистрировать платёж"
    :style="{ width: '680px' }"
    :breakpoints="{ '720px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="save">
      <div v-if="!contractId" class="field">
        <label for="pd-contract">Договор *</label>
        <Select
          v-model="form.contractId"
          input-id="pd-contract"
          :options="contractOptions"
          option-label="label"
          option-value="value"
          filter
          filter-placeholder="Номер, наниматель, объект"
          :invalid="!!err('contractId')"
          fluid
        />
        <small v-if="err('contractId')" class="error">{{ err('contractId') }}</small>
      </div>
      <div class="field">
        <label id="pd-purpose">Назначение *</label>
        <SelectButton
          v-model="form.purpose"
          :options="options(paymentPurposeFormLabels)"
          option-label="label"
          option-value="value"
          :allow-empty="false"
          aria-labelledby="pd-purpose"
        />
      </div>
      <div class="form-grid">
        <div class="field">
          <label for="pd-amount">Сумма *</label>
          <MoneyInput
            v-model="form.amountKop"
            input-id="pd-amount"
            :invalid="!!err('amountKop')"
            @error="(e) => (amountError = e)"
          />
          <small v-if="err('amountKop')" class="error">{{ err('amountKop') }}</small>
        </div>
        <div class="field">
          <label for="pd-date">Дата поступления *</label>
          <DateInput
            v-model="form.receivedOn"
            input-id="pd-date"
            :max-date="new Date()"
            :invalid="!!err('receivedOn')"
          />
          <small v-if="err('receivedOn')" class="error">{{ err('receivedOn') }}</small>
        </div>
      </div>
      <div class="field">
        <label id="pd-method">Способ *</label>
        <SelectButton
          v-model="form.method"
          :options="options(manualPaymentMethodLabels)"
          option-label="label"
          option-value="value"
          :allow-empty="false"
          aria-labelledby="pd-method"
        />
      </div>

      <template v-if="form.purpose === 'charges' && form.contractId">
        <div>
          <Button
            type="button"
            :label="pickCharges ? 'Скрыть начисления' : 'Выбрать начисления'"
            :icon="pickCharges ? 'pi pi-chevron-up' : 'pi pi-chevron-down'"
            text
            @click="pickCharges = !pickCharges"
          />
          <span v-if="form.chargeIds.length" class="hint">выбрано: {{ form.chargeIds.length }}</span>
          <span v-else class="hint">если ничего не выбрано — по порядку погашения</span>
        </div>
        <DataTable
          v-if="pickCharges"
          v-model:selection="selected"
          :value="openCharges"
          :loading="charges.isFetching.value"
          data-key="id"
          size="small"
          scrollable
          scroll-height="16rem"
        >
          <template #empty>Открытых начислений нет</template>
          <Column selection-mode="multiple" header-style="width: 3rem" />
          <Column header="Начисление" field="title" />
          <Column header="Срок">
            <template #body="{ data }">{{ formatDate(data.dueDate) }}</template>
          </Column>
          <Column header="Остаток">
            <template #body="{ data }"><MoneyText :kop="data.outstandingKop" /></template>
          </Column>
        </DataTable>
      </template>

      <div class="field">
        <label for="pd-comment">Комментарий</label>
        <Textarea id="pd-comment" v-model="form.comment" rows="2" auto-resize maxlength="1000" fluid />
      </div>

      <section v-if="preview" class="preview" aria-label="Предпросмотр распределения">
        <h4>Распределение (предварительно)</h4>
        <div v-for="l in preview.lines" :key="l.charge.id" class="row">
          <span>{{ l.charge.title }}</span>
          <MoneyText :kop="l.amountKop" />
        </div>
        <div v-if="preview.advanceKop > 0" class="row">
          <span>Аванс</span>
          <MoneyText :kop="preview.advanceKop" />
        </div>
      </section>
      <p v-else-if="form.purpose === 'deposit'" class="hint">
        Платёж будет учтён как внесение обеспечительного платежа и на начисления не распределяется.
      </p>

      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button type="submit" label="Зарегистрировать" :loading="m.create.isPending.value" />
      </div>
    </form>
  </Dialog>
</template>

<style scoped>
.preview {
  border: 1px solid var(--p-content-border-color);
  border-radius: 6px;
  padding: 0.5rem 0.75rem;
}
.preview h4 {
  margin: 0 0 0.5rem;
}
.row {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.125rem 0;
}
</style>
