<script setup lang="ts">
// Диалог «Разобрать» расхождение (14 Экраны собственника, раздел 7; 05 разд. 10.6, 15.2).
// Переплата: «Оставить авансом», «Вернуть» (сумма, дата, способ), «Перенести в залог» (сумма); недоплата:
// «Принять к сведению» (комментарий). Неопознанные платежи разбираются вместе с выписками (этап M9).
import dayjs from 'dayjs'
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useDiscrepancyMutations, type Discrepancy } from '@/api/queries/payments'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import { discrepancyKindLabels, options, refundMethodLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'

type Action = 'keep_advance' | 'refund' | 'move_to_deposit' | 'acknowledge'

const props = defineProps<{ discrepancy: Discrepancy }>()
const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()
const m = useDiscrepancyMutations()
const today = () => dayjs().format('YYYY-MM-DD')

const overActions: { value: Action; label: string }[] = [
  { value: 'keep_advance', label: 'Оставить авансом' },
  { value: 'refund', label: 'Вернуть' },
  { value: 'move_to_deposit', label: 'Перенести в залог' },
]
const form = reactive({
  action: 'keep_advance' as Action,
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
      action: props.discrepancy.kind === 'underpayment' ? 'acknowledge' : 'keep_advance',
      amountKop: props.discrepancy.amountKop,
      refundedOn: today(),
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
    else if (form.refundedOn > today()) e.refundedOn = 'Дата не может быть позже сегодняшней'
  }
  if (form.action === 'acknowledge' && !form.note.trim()) e.note = 'Укажите комментарий'
  return { ...e, ...serverErrors.value }
})
const err = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(errors.value).length) return
  try {
    await m.resolve.mutateAsync({
      id: props.discrepancy.id,
      body: {
        action: form.action,
        amountKop: needsAmount.value ? form.amountKop : null,
        refundedOn: form.action === 'refund' ? form.refundedOn : null,
        method: form.action === 'refund' ? form.method : null,
        note: form.note.trim() || null,
      },
    })
    visible.value = false
    toast.add({ severity: 'success', summary: 'Расхождение разобрано', life: 3000 })
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
    <Message v-if="discrepancy.kind === 'unidentified'" severity="info" :closable="false">
      Разбор неопознанных платежей будет доступен вместе со сверкой выписок (этап M9 плана реализации).
    </Message>
    <form v-else class="form" novalidate @submit.prevent="save">
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
      <div v-if="discrepancy.kind === 'overpayment'" class="field">
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
          <DateInput
            v-model="form.refundedOn"
            input-id="rd-date"
            :max-date="new Date()"
            :invalid="!!err('refundedOn')"
          />
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
