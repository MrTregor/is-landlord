<script setup lang="ts">
// Диалог «Разобрать» расхождение (14 Экраны собственника, раздел 7; 05 разд. 10.6, 15.2).
// Переплата: «Оставить авансом», «Вернуть» (сумма, дата, способ), «Перенести в залог» (сумма); недоплата:
// «Принять к сведению» (комментарий); неопознанный: «Привязать к договору» (подсказки — договоры, где наниматель
// похож на плательщика), «Возвращён плательщику», «Не относится к аренде».
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useContracts } from '@/api/queries/contracts'
import { useDiscrepancyMutations, type Discrepancy } from '@/api/queries/payments'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import { discrepancyKindLabels, options, refundMethodLabels } from '@/utils/labels'
import { formatKop } from '@/utils/money'
import { formatDate } from '@/utils/date'
import { parseProblem } from '@/utils/problem'
import { today, todayIso } from '@/utils/today'
import { groupContracts } from '../suggest'

type Action = 'keep_advance' | 'refund' | 'move_to_deposit' | 'acknowledge' | 'link' | 'returned_to_payer' | 'ignore'

const props = defineProps<{ discrepancy: Discrepancy }>()
const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()
const m = useDiscrepancyMutations()

const overActions: { value: Action; label: string }[] = [
  { value: 'keep_advance', label: 'Оставить авансом' },
  { value: 'refund', label: 'Вернуть' },
  { value: 'move_to_deposit', label: 'Перенести в залог' },
]
const unidentifiedActions: { value: Action; label: string }[] = [
  { value: 'link', label: 'Привязать к договору' },
  { value: 'returned_to_payer', label: 'Возвращён плательщику' },
  { value: 'ignore', label: 'Не относится к аренде' },
]
const isUnidentified = computed(() => props.discrepancy.kind === 'unidentified')
const contracts = useContracts(() => ({
  status: ['active', 'extended', 'terminated', 'finished'],
  pageSize: 100,
}))
const contractGroups = computed(() =>
  groupContracts(
    props.discrepancy.bankLine?.payerName,
    (contracts.data.value?.items ?? []).map((c) => ({
      value: c.id,
      label: `№ ${c.number} — ${c.property.code}, ${c.tenant.displayName}`,
      tenant: c.tenant.displayName,
    })),
  ),
)

const form = reactive({
  action: 'keep_advance' as Action,
  contractId: null as string | null,
  amountKop: null as number | null,
  refundedOn: null as string | null,
  method: 'bank_transfer' as 'bank_transfer' | 'cash',
  note: '',
})
const amountError = ref<string | null>(null)
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
watch(
  visible,
  (v) => {
    if (!v) return
    Object.assign(form, {
      action: { underpayment: 'acknowledge', overpayment: 'keep_advance', unidentified: 'link' }[
        props.discrepancy.kind
      ] as Action,
      contractId: null,
      amountKop: props.discrepancy.amountKop,
      refundedOn: todayIso(),
      method: 'bank_transfer',
      note: '',
    })
    amountError.value = null
    submitted.value = false
    serverErrors.value = {}
    serverError.value = null
  },
  { immediate: true },
)

const needsAmount = computed(() => form.action === 'refund' || form.action === 'move_to_deposit')
const errors = computed(() => {
  const e: Record<string, string> = {}
  if (needsAmount.value) {
    if (amountError.value) e.amountKop = amountError.value
    else if (!form.amountKop || form.amountKop <= 0) e.amountKop = 'Сумма должна быть больше нуля'
    else if (form.amountKop > props.discrepancy.amountKop) e.amountKop = 'Сумма больше переплаты'
  }
  if (form.action === 'refund') {
    if (!form.refundedOn) e.refundedOn = 'Укажите дату возврата'
    else if (form.refundedOn > todayIso()) e.refundedOn = 'Дата не может быть позже сегодняшней'
  }
  if (form.action === 'acknowledge' && !form.note.trim()) e.note = 'Укажите комментарий'
  if (form.action === 'link' && !form.contractId) e.contractId = 'Выберите договор'
  return { ...e, ...serverErrors.value }
})
const err = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(errors.value).length) return
  try {
    const res = await m.resolve.mutateAsync({
      id: props.discrepancy.id,
      body: {
        action: form.action,
        contractId: form.action === 'link' ? form.contractId : null,
        amountKop: needsAmount.value ? form.amountKop : null,
        refundedOn: form.action === 'refund' ? form.refundedOn : null,
        method: form.action === 'refund' ? form.method : null,
        note: form.note.trim() || null,
      },
    })
    visible.value = false
    // Частичный возврат или перенос не закрывает расхождение: остаток переплаты отслеживается дальше (05 разд. 10.6).
    toast.add({
      severity: 'success',
      summary: res.status === 'open' ? `Переплата уменьшена до ${formatKop(res.amountKop)}` : 'Расхождение разобрано',
      life: 4000,
    })
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
    :header="`Разобрать: ${discrepancyKindLabels[discrepancy.kind]}`"
    :style="{ width: '520px' }"
    :breakpoints="{ '560px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="save">
      <p>
        Сумма: <strong><MoneyText :kop="discrepancy.amountKop" /></strong>
        <template v-if="discrepancy.contract">
          · договор
          <RouterLink :to="`/contracts/${discrepancy.contract.id}`">№ {{ discrepancy.contract.number }}</RouterLink>
        </template>
        <template v-if="discrepancy.paymentId">
          · <RouterLink :to="`/payments/${discrepancy.paymentId}`">платёж {{ discrepancy.receiptNumber }}</RouterLink>
        </template>
      </p>
      <dl v-if="isUnidentified && discrepancy.bankLine" class="props">
        <dt>Дата поступления</dt>
        <dd>{{ formatDate(discrepancy.bankLine.receivedOn) }}</dd>
        <dt>Плательщик</dt>
        <dd>
          {{ discrepancy.bankLine.payerName ?? '—' }}
          <template v-if="discrepancy.bankLine.payerInn">, ИНН {{ discrepancy.bankLine.payerInn }}</template>
        </dd>
        <dt>Назначение</dt>
        <dd>{{ discrepancy.bankLine.purpose ?? '—' }}</dd>
      </dl>
      <template v-if="isUnidentified">
        <div class="field">
          <label id="rd-uaction">Решение *</label>
          <SelectButton
            v-model="form.action"
            :options="unidentifiedActions"
            option-label="label"
            option-value="value"
            :allow-empty="false"
            aria-labelledby="rd-uaction"
          />
        </div>
        <div v-if="form.action === 'link'" class="field">
          <label for="rd-contract">Договор *</label>
          <Select
            v-model="form.contractId"
            input-id="rd-contract"
            :options="contractGroups"
            option-label="label"
            option-value="value"
            option-group-label="label"
            option-group-children="items"
            filter
            filter-placeholder="Номер, наниматель, объект"
            :loading="contracts.isFetching.value"
            :invalid="!!err('contractId')"
            fluid
          />
          <small v-if="err('contractId')" class="error">{{ err('contractId') }}</small>
          <small class="hint">По строке будет создан платёж «из выписки» и распределён на начисления договора.</small>
        </div>
      </template>
      <div v-else-if="discrepancy.kind === 'overpayment'" class="field">
        <label id="rd-action">Решение *</label>
        <SelectButton
          v-model="form.action"
          :options="overActions"
          option-label="label"
          option-value="value"
          :allow-empty="false"
          aria-labelledby="rd-action"
        />
      </div>
      <p v-else class="hint">
        Принять к сведению: при новом платеже с долгом будет создано новое расхождение.
        <RouterLink v-if="discrepancy.contract" :to="`/contracts/${discrepancy.contract.id}?tab=charges`"
          >Открыть начисления договора</RouterLink
        >
      </p>
      <div v-if="needsAmount" class="form-grid">
        <div class="field">
          <label for="rd-amount">Сумма *</label>
          <MoneyInput
            v-model="form.amountKop"
            input-id="rd-amount"
            :invalid="!!err('amountKop')"
            @error="(e) => (amountError = e)"
          />
          <small v-if="err('amountKop')" class="error">{{ err('amountKop') }}</small>
        </div>
        <div v-if="form.action === 'refund'" class="field">
          <label for="rd-date">Дата возврата *</label>
          <DateInput v-model="form.refundedOn" input-id="rd-date" :max-date="today()" :invalid="!!err('refundedOn')" />
          <small v-if="err('refundedOn')" class="error">{{ err('refundedOn') }}</small>
        </div>
      </div>
      <div v-if="form.action === 'refund'" class="field">
        <label id="rd-method">Способ *</label>
        <SelectButton
          v-model="form.method"
          :options="options(refundMethodLabels)"
          option-label="label"
          option-value="value"
          :allow-empty="false"
          aria-labelledby="rd-method"
        />
      </div>
      <div class="field">
        <label for="rd-note">{{ form.action === 'acknowledge' ? 'Комментарий *' : 'Комментарий' }}</label>
        <Textarea
          id="rd-note"
          v-model="form.note"
          rows="2"
          auto-resize
          maxlength="1000"
          :invalid="!!err('note')"
          fluid
        />
        <small v-if="err('note')" class="error">{{ err('note') }}</small>
      </div>
      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button type="submit" label="Сохранить" :loading="m.resolve.isPending.value" />
      </div>
    </form>
  </Dialog>
</template>

<style scoped>
.props {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.25rem 1rem;
  margin: 0;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
