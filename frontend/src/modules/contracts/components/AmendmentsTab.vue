<script setup lang="ts">
// Вкладка «Допсоглашения» (14 Экраны собственника, раздел 4.3, вкладка 6; 05 разд. 8.3).
import dayjs from 'dayjs'
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import {
  fetchAmendments,
  useContractMutations,
  useContractSub,
  type Amendment,
  type ContractDetail,
} from '@/api/queries/contracts'
import { documentPdfUrl, useDocumentMutations } from '@/api/queries/documents'
import type { components } from '@/api/schema'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatMonth, toIsoDate } from '@/utils/date'
import { amendmentKindLabels, amendmentStatusLabels, amendmentStatusSeverity, options } from '@/utils/labels'
import { formatKop } from '@/utils/money'
import { openInNewTab } from '@/utils/pdf'
import { parseProblem } from '@/utils/problem'

type Kind = components['schemas']['AmendmentKind']
const props = defineProps<{ contract: ContractDetail; createSignal: number }>()
const toast = useToast()
const list = useContractSub(() => props.contract.id, 'amendments', fetchAmendments)
const m = useContractMutations(() => props.contract.id)
const docs = useDocumentMutations()

/** PDF допсоглашения: у черновика формируется заново, у подписанного — открывается последний сформированный. */
async function amendmentPdf(a: Amendment) {
  try {
    await openInNewTab(async () =>
      a.status !== 'draft' && a.documentId
        ? documentPdfUrl(a.documentId)
        : documentPdfUrl((await docs.amendment.mutateAsync(a.id)).id),
    )
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}
const live = computed(() => props.contract.status === 'active' || props.contract.status === 'extended')

/** Кратко об изменениях: «плата 29 400,00 ₽», «до 30.11.2027». */
function changes(a: Amendment): string {
  const parts: string[] = []
  if (a.newRentKop != null) parts.push(`плата ${formatKop(a.newRentKop)}`)
  if (a.newPaymentDay != null) parts.push(`день оплаты ${a.newPaymentDay}`)
  if (a.newUtilitiesFixedKop != null) parts.push(`фиксированная часть ЖКУ ${formatKop(a.newUtilitiesFixedKop)}`)
  if (a.newEndDate) parts.push(`срок до ${formatDate(a.newEndDate)}`)
  if (a.bodyText) parts.push(a.bodyText.length > 60 ? a.bodyText.slice(0, 60) + '…' : a.bodyText)
  return parts.join('; ')
}

// Диалог создания и правки.
const dialog = ref(false)
const editing = ref<Amendment | null>(null)
const form = reactive({
  kind: 'rent_change' as Kind,
  month: null as Date | null,
  effectiveFrom: null as string | null,
  newRentKop: null as number | null,
  newPaymentDay: null as number | null,
  newUtilitiesFixedKop: null as number | null,
  newEndDate: null as string | null,
  bodyText: '',
})
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
const byMonth = computed(() => ['indexation', 'rent_change', 'terms_change'].includes(form.kind))

function open(a: Amendment | null) {
  editing.value = a
  const nextMonth = dayjs().add(1, 'month').startOf('month')
  Object.assign(form, {
    kind: a?.kind ?? 'rent_change',
    month: a ? dayjs(a.effectiveFrom).toDate() : nextMonth.toDate(),
    effectiveFrom: a?.effectiveFrom ?? dayjs(props.contract.endDate).add(1, 'day').format('YYYY-MM-DD'),
    newRentKop: a?.newRentKop ?? null,
    newPaymentDay: a?.newPaymentDay ?? null,
    newUtilitiesFixedKop: a?.newUtilitiesFixedKop ?? null,
    newEndDate: a?.newEndDate ?? null,
    bodyText: a?.bodyText ?? '',
  })
  if (!a && form.kind === 'rent_change') form.effectiveFrom = null
  submitted.value = false
  serverErrors.value = {}
  serverError.value = null
  dialog.value = true
}
watch(
  () => props.createSignal,
  (n) => {
    if (n) open(null)
  },
)

const errors = computed(() => {
  const e: Record<string, string> = {}
  if (byMonth.value && !form.month) e.effectiveFrom = 'Выберите месяц'
  if (!byMonth.value && !form.effectiveFrom) e.effectiveFrom = 'Укажите дату'
  if ((form.kind === 'indexation' || form.kind === 'rent_change') && !(form.newRentKop && form.newRentKop > 0))
    e.newRentKop = 'Укажите новую плату'
  if (form.kind === 'extension' && !form.newEndDate) e.newEndDate = 'Укажите новую дату окончания'
  if (form.kind === 'terms_change' && form.newPaymentDay == null && form.newUtilitiesFixedKop == null)
    e.newPaymentDay = 'Укажите новый день оплаты или фиксированную часть ЖКУ'
  if (form.kind === 'other' && !form.bodyText.trim()) e.bodyText = 'Опишите изменения условий'
  return { ...e, ...serverErrors.value }
})
const showErr = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  if (Object.keys(errors.value).length) return
  const k = form.kind
  const body: components['schemas']['AmendmentInput'] = {
    kind: k,
    effectiveFrom: byMonth.value ? toIsoDate(dayjs(form.month!).startOf('month').toDate()) : form.effectiveFrom!,
    newRentKop: k === 'indexation' || k === 'rent_change' ? form.newRentKop : null,
    newPaymentDay: k === 'terms_change' ? form.newPaymentDay : null,
    newUtilitiesFixedKop: k === 'terms_change' ? form.newUtilitiesFixedKop : null,
    newEndDate: k === 'extension' ? form.newEndDate : null,
    bodyText: form.bodyText.trim() || null,
  }
  try {
    if (editing.value) await m.updateAmendment.mutateAsync({ id: editing.value.id, body })
    else await m.createAmendment.mutateAsync(body)
    dialog.value = false
    toast.add({ severity: 'success', summary: 'Допсоглашение сохранено', life: 3000 })
  } catch (err) {
    const p = parseProblem(err)
    serverErrors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}

// «Подписано» и «Отменить».
const signDialog = ref(false)
const signTarget = ref<Amendment | null>(null)
const signedOn = ref<string | null>(null)
const signError = ref<string | null>(null)
function openSign(a: Amendment) {
  signTarget.value = a
  signedOn.value = toIsoDate(new Date())
  signError.value = null
  signDialog.value = true
}
async function sign() {
  if (!signedOn.value || !signTarget.value) {
    signError.value = 'Укажите дату подписания'
    return
  }
  try {
    await m.signAmendment.mutateAsync({ id: signTarget.value.id, signedOn: signedOn.value })
    signDialog.value = false
    toast.add({ severity: 'success', summary: 'Допсоглашение подписано, условия применены', life: 3000 })
  } catch (err) {
    signError.value = parseProblem(err).message
  }
}

const cancelDialog = ref(false)
const cancelTarget = ref<Amendment | null>(null)
const cancelReason = ref('')
const cancelSubmitted = ref(false)
const cancelError = ref<string | null>(null)
function openCancel(a: Amendment) {
  cancelTarget.value = a
  cancelReason.value = ''
  cancelSubmitted.value = false
  cancelError.value = null
  cancelDialog.value = true
}
async function cancel() {
  cancelSubmitted.value = true
  if (cancelReason.value.trim().length < 5 || !cancelTarget.value) return
  try {
    await m.cancelAmendment.mutateAsync({ id: cancelTarget.value.id, reason: cancelReason.value.trim() })
    cancelDialog.value = false
    toast.add({ severity: 'success', summary: 'Допсоглашение отменено', life: 3000 })
  } catch (err) {
    cancelError.value = parseProblem(err).message
  }
}

const kindOptions = options(amendmentKindLabels)
const monthLabel = computed(() => (form.month ? formatMonth(toIsoDate(form.month)) : ''))
</script>

<template>
  <div>
    <div v-if="live" class="actions actions--start toolbar">
      <Button label="Новое допсоглашение" icon="pi pi-plus" @click="open(null)" />
    </div>
    <DataTable :value="list.data.value ?? []" :loading="list.isFetching.value" size="small" data-key="id">
      <template #empty>Допсоглашений нет</template>
      <Column header="№" field="number" />
      <Column header="Вид">
        <template #body="{ data }">
          {{ amendmentKindLabels[(data as Amendment).kind] }}
          <Tag v-if="(data as Amendment).autoCreated" value="авто" severity="secondary" />
        </template>
      </Column>
      <Column header="С даты">
        <template #body="{ data }">{{ formatDate((data as Amendment).effectiveFrom) }}</template>
      </Column>
      <Column header="Изменения">
        <template #body="{ data }">{{ changes(data as Amendment) }}</template>
      </Column>
      <Column header="Статус">
        <template #body="{ data }">
          <StatusTag
            :label="amendmentStatusLabels[(data as Amendment).status]"
            :severity="amendmentStatusSeverity[(data as Amendment).status]"
          />
          <div v-if="(data as Amendment).cancelReason" class="hint">{{ (data as Amendment).cancelReason }}</div>
        </template>
      </Column>
      <Column header="Подписано">
        <template #body="{ data }">{{ formatDate((data as Amendment).signedOn) || '—' }}</template>
      </Column>
      <Column header="">
        <template #body="{ data }">
          <div v-if="(data as Amendment).status === 'draft'" class="row-actions">
            <Button label="Изменить" size="small" text :disabled="!live" @click="open(data as Amendment)" />
            <Button label="PDF" size="small" text @click="amendmentPdf(data as Amendment)" />
            <Button label="Подписано" size="small" text :disabled="!live" @click="openSign(data as Amendment)" />
            <Button label="Отменить" size="small" text severity="danger" @click="openCancel(data as Amendment)" />
          </div>
          <div v-else-if="(data as Amendment).status === 'signed'" class="row-actions">
            <Button label="PDF" size="small" text @click="amendmentPdf(data as Amendment)" />
          </div>
        </template>
      </Column>
    </DataTable>
    <p class="hint">После подписания уже созданные начисления с месяца вступления пересчитываются.</p>

    <Dialog
      v-model:visible="dialog"
      modal
      :header="editing ? `Допсоглашение № ${editing.number}` : 'Новое допсоглашение'"
      :style="{ width: '560px' }"
      :breakpoints="{ '600px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="save">
        <div class="field">
          <label for="am-kind">Вид *</label>
          <Select
            v-model="form.kind"
            input-id="am-kind"
            :options="kindOptions"
            option-label="label"
            option-value="value"
            fluid
          />
        </div>
        <div v-if="byMonth" class="field">
          <label for="am-month">С месяца * <span class="hint">(изменения вступают с первого числа)</span></label>
          <DatePicker
            v-model="form.month"
            input-id="am-month"
            view="month"
            date-format="mm.yy"
            show-icon
            icon-display="input"
            :invalid="!!showErr('effectiveFrom')"
            fluid
          />
          <small v-if="showErr('effectiveFrom')" class="error">{{ showErr('effectiveFrom') }}</small>
          <small v-else-if="monthLabel" class="hint">С 1 числа: {{ monthLabel }}</small>
        </div>
        <div v-else class="field">
          <label for="am-from">С даты *</label>
          <DateInput v-model="form.effectiveFrom" input-id="am-from" :invalid="!!showErr('effectiveFrom')" />
          <small v-if="showErr('effectiveFrom')" class="error">{{ showErr('effectiveFrom') }}</small>
        </div>
        <div v-if="form.kind === 'indexation' || form.kind === 'rent_change'" class="field">
          <label for="am-rent">Новая плата в месяц *</label>
          <MoneyInput v-model="form.newRentKop" input-id="am-rent" :invalid="!!showErr('newRentKop')" />
          <small v-if="showErr('newRentKop')" class="error">{{ showErr('newRentKop') }}</small>
          <small v-else class="hint">Сейчас: {{ formatKop(contract.currentTerms.rentKop) }}</small>
        </div>
        <div v-if="form.kind === 'extension'" class="field">
          <label for="am-end">Новая дата окончания *</label>
          <DateInput v-model="form.newEndDate" input-id="am-end" :invalid="!!showErr('newEndDate')" />
          <small v-if="showErr('newEndDate')" class="error">{{ showErr('newEndDate') }}</small>
          <small v-else class="hint">Сейчас: до {{ formatDate(contract.endDate) }}</small>
        </div>
        <template v-if="form.kind === 'terms_change'">
          <div class="field">
            <label for="am-day">Новый день оплаты</label>
            <InputNumber
              v-model="form.newPaymentDay"
              input-id="am-day"
              :min="1"
              :max="28"
              :invalid="!!showErr('newPaymentDay')"
              fluid
            />
            <small v-if="showErr('newPaymentDay')" class="error">{{ showErr('newPaymentDay') }}</small>
          </div>
          <div class="field">
            <label for="am-fixed">Новая фиксированная часть ЖКУ</label>
            <MoneyInput
              v-model="form.newUtilitiesFixedKop"
              input-id="am-fixed"
              :invalid="!!showErr('newUtilitiesFixedKop')"
            />
            <small v-if="showErr('newUtilitiesFixedKop')" class="error">{{ showErr('newUtilitiesFixedKop') }}</small>
          </div>
        </template>
        <div class="field">
          <label for="am-text">Текст{{ form.kind === 'other' ? ' *' : '' }}</label>
          <Textarea id="am-text" v-model="form.bodyText" rows="3" auto-resize :invalid="!!showErr('bodyText')" fluid />
          <small v-if="showErr('bodyText')" class="error">{{ showErr('bodyText') }}</small>
        </div>
        <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="dialog = false" />
          <Button
            type="submit"
            label="Сохранить"
            :loading="m.createAmendment.isPending.value || m.updateAmendment.isPending.value"
          />
        </div>
      </form>
    </Dialog>

    <Dialog
      v-model:visible="signDialog"
      modal
      header="Допсоглашение подписано"
      :style="{ width: '440px' }"
      :breakpoints="{ '480px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="sign">
        <div class="field">
          <label for="am-signed">Дата подписания *</label>
          <DateInput v-model="signedOn" input-id="am-signed" :max-date="new Date()" />
        </div>
        <p class="hint">Изменения применяются к договору сразу; нанимателю уйдёт письмо, если меняется плата.</p>
        <Message v-if="signError" severity="error" :closable="false">{{ signError }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="signDialog = false" />
          <Button type="submit" label="Подписано" :loading="m.signAmendment.isPending.value" />
        </div>
      </form>
    </Dialog>

    <Dialog
      v-model:visible="cancelDialog"
      modal
      header="Отменить допсоглашение"
      :style="{ width: '440px' }"
      :breakpoints="{ '480px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="cancel">
        <div class="field">
          <label for="am-reason">Причина *</label>
          <Textarea
            id="am-reason"
            v-model="cancelReason"
            rows="2"
            auto-resize
            :invalid="cancelSubmitted && cancelReason.trim().length < 5"
            fluid
          />
          <small v-if="cancelSubmitted && cancelReason.trim().length < 5" class="error"
            >Причина — не короче 5 символов</small
          >
        </div>
        <Message v-if="cancelError" severity="error" :closable="false">{{ cancelError }}</Message>
        <div class="actions">
          <Button type="button" label="Назад" outlined @click="cancelDialog = false" />
          <Button
            type="submit"
            label="Отменить допсоглашение"
            severity="danger"
            :loading="m.cancelAmendment.isPending.value"
          />
        </div>
      </form>
    </Dialog>
  </div>
</template>

<style scoped>
.toolbar {
  margin-bottom: 0.75rem;
}
.actions--start {
  justify-content: flex-start;
}
.row-actions {
  display: flex;
  flex-wrap: wrap;
}
</style>
