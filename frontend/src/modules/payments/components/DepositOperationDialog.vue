<script setup lang="ts">
// Операции с залогом (05 разд. 9, 14 разд. 4.3, вкладка «Залог»): удержание, зачёт в счёт долга, возврат.
// В диалоге зачёта — открытые начисления с флажками; по умолчанию отмечены по порядку погашения до суммы остатка.
// Удержание можно связать с заявкой объекта договора (05 разд. 9.2); затрата за счёт нанимателя оформляет
// удержание сама — из диалога затраты.
import dayjs from 'dayjs'
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useCharges, type Charge } from '@/api/queries/charges'
import { useContract } from '@/api/queries/contracts'
import { useRequests } from '@/api/queries/maintenance'
import { useDepositMutations } from '@/api/queries/payments'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import { formatDate } from '@/utils/date'
import { options, refundMethodLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { allocationOrder } from '../allocation'
import type { DepositOpKind } from '../types'

const props = defineProps<{ contractId: string; balanceKop: number; kind: DepositOpKind }>()
const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()
const m = useDepositMutations()
const today = () => dayjs().format('YYYY-MM-DD')

const titles: Record<DepositOpKind, string> = {
  withhold: 'Удержать из залога',
  offset: 'Зачесть залог в счёт долга',
  refund: 'Вернуть залог',
}
const done: Record<DepositOpKind, string> = {
  withhold: 'Удержание из залога оформлено',
  offset: 'Залог зачтён в счёт долга',
  refund: 'Возврат залога оформлен',
}

const form = reactive({
  amountKop: null as number | null,
  opDate: null as string | null,
  reason: '',
  method: 'bank_transfer' as 'bank_transfer' | 'cash',
  chargeIds: [] as string[],
  maintenanceRequestId: null as string | null,
})
const contract = useContract(() => props.contractId)
const requests = useRequests(
  () => ({ propertyId: contract.data.value?.property.id, pageSize: 100 }),
  () => visible.value && props.kind === 'withhold' && !!contract.data.value,
)
const requestOptions = computed(() =>
  (requests.data.value?.items ?? []).map((r) => ({ value: r.id, label: `${r.number} — ${r.title}` })),
)
const amountError = ref<string | null>(null)
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)

const charges = useCharges(
  () => ({ contractId: props.contractId, status: ['open', 'partially_paid'], sort: 'dueDate', pageSize: 100 }),
  () => visible.value && props.kind === 'offset',
)
const ordered = computed<Charge[]>(() => allocationOrder(charges.data.value?.items ?? []))
const selected = computed({
  get: () => ordered.value.filter((c) => form.chargeIds.includes(c.id)),
  set: (rows: Charge[]) => {
    form.chargeIds = rows.map((r) => r.id)
    form.amountKop = Math.min(props.balanceKop, debtOfSelected.value) || null
  },
})
const debtOfSelected = computed(() =>
  ordered.value.filter((c) => form.chargeIds.includes(c.id)).reduce((s, c) => s + c.outstandingKop, 0),
)

/** По умолчанию отмечены начисления по порядку погашения, пока хватает остатка залога. */
function defaultSelection() {
  let rest = props.balanceKop
  const ids: string[] = []
  for (const c of ordered.value) {
    if (rest <= 0) break
    ids.push(c.id)
    rest -= c.outstandingKop
  }
  form.chargeIds = ids
  form.amountKop = Math.min(props.balanceKop, debtOfSelected.value) || null
}

watch(visible, (v) => {
  if (!v) return
  Object.assign(form, {
    amountKop: props.kind === 'refund' ? props.balanceKop || null : null,
    opDate: today(),
    reason: '',
    method: 'bank_transfer',
    chargeIds: [],
    maintenanceRequestId: null,
  })
  if (props.kind === 'offset') defaultSelection()
  amountError.value = null
  submitted.value = false
  serverErrors.value = {}
  serverError.value = null
})
watch(
  () => charges.data.value,
  () => {
    if (visible.value && props.kind === 'offset' && !form.chargeIds.length) defaultSelection()
  },
)

const errors = computed(() => {
  const e: Record<string, string> = {}
  if (amountError.value) e.amountKop = amountError.value
  else if (!form.amountKop || form.amountKop <= 0) e.amountKop = 'Сумма должна быть больше нуля'
  else if (form.amountKop > props.balanceKop) e.amountKop = 'Сумма больше остатка залога'
  else if (props.kind === 'offset' && form.amountKop > debtOfSelected.value)
    e.amountKop = 'Сумма больше долга по выбранным начислениям'
  if (!form.opDate) e.opDate = 'Укажите дату'
  else if (form.opDate > today()) e.opDate = 'Дата не может быть позже сегодняшней'
  if (!form.reason.trim()) e.reason = 'Укажите причину'
  if (props.kind === 'offset' && !form.chargeIds.length) e.chargeIds = 'Выберите начисления'
  return { ...e, ...serverErrors.value }
})
const err = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(errors.value).length) return
  const base = { amountKop: form.amountKop!, opDate: form.opDate!, reason: form.reason.trim() }
  try {
    if (props.kind === 'withhold')
      await m.withhold.mutateAsync({
        contractId: props.contractId,
        body: { ...base, maintenanceRequestId: form.maintenanceRequestId },
      })
    else if (props.kind === 'offset')
      await m.offset.mutateAsync({ contractId: props.contractId, body: { ...base, chargeIds: form.chargeIds } })
    else await m.refund.mutateAsync({ contractId: props.contractId, body: { ...base, method: form.method } })
    visible.value = false
    toast.add({ severity: 'success', summary: done[props.kind], life: 3000 })
  } catch (e) {
    const p = parseProblem(e)
    serverErrors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}
const pending = computed(() => m.withhold.isPending.value || m.offset.isPending.value || m.refund.isPending.value)
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    :header="titles[kind]"
    :style="{ width: kind === 'offset' ? '640px' : '480px' }"
    :breakpoints="{ '680px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="save">
      <p class="hint">Остаток залога: <MoneyText :kop="balanceKop" /></p>
      <template v-if="kind === 'offset'">
        <DataTable
          v-model:selection="selected"
          :value="ordered"
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
        <small v-if="err('chargeIds')" class="error">{{ err('chargeIds') }}</small>
        <p class="hint">Долг по выбранным: <MoneyText :kop="debtOfSelected" /></p>
      </template>
      <div class="form-grid">
        <div class="field">
          <label for="do-amount">Сумма *</label>
          <MoneyInput
            v-model="form.amountKop"
            input-id="do-amount"
            :invalid="!!err('amountKop')"
            @error="(e) => (amountError = e)"
          />
          <small v-if="err('amountKop')" class="error">{{ err('amountKop') }}</small>
        </div>
        <div class="field">
          <label for="do-date">Дата *</label>
          <DateInput v-model="form.opDate" input-id="do-date" :max-date="new Date()" :invalid="!!err('opDate')" />
          <small v-if="err('opDate')" class="error">{{ err('opDate') }}</small>
        </div>
      </div>
      <div v-if="kind === 'refund'" class="field">
        <label id="do-method">Способ *</label>
        <SelectButton
          v-model="form.method"
          :options="options(refundMethodLabels)"
          option-label="label"
          option-value="value"
          :allow-empty="false"
          aria-labelledby="do-method"
        />
      </div>
      <div v-if="kind === 'withhold'" class="field">
        <label for="do-request">Заявка</label>
        <Select
          v-model="form.maintenanceRequestId"
          input-id="do-request"
          :options="requestOptions"
          option-label="label"
          option-value="value"
          filter
          show-clear
          placeholder="Без заявки"
          fluid
        />
      </div>
      <div class="field">
        <label for="do-reason">{{ kind === 'withhold' ? 'Причина удержания *' : 'Основание *' }}</label>
        <Textarea id="do-reason" v-model="form.reason" rows="2" auto-resize :invalid="!!err('reason')" fluid />
        <small v-if="err('reason')" class="error">{{ err('reason') }}</small>
      </div>
      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button type="submit" label="Сохранить" :loading="pending" />
      </div>
    </form>
  </Dialog>
</template>
