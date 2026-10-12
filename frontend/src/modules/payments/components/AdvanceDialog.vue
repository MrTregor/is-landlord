<script setup lang="ts">
// Аванс платежа (05 разд. 10.6, 14 разд. 6.3): «Вернуть» (сумма, дата, способ, причина) или «Перенести в залог»
// (сумма, дата). Сумма — не больше аванса.
import dayjs from 'dayjs'
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { usePaymentMutations } from '@/api/queries/payments'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import { options, refundMethodLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'

const props = defineProps<{ paymentId: string; advanceKop: number; mode: 'refund' | 'deposit' }>()
const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()
const m = usePaymentMutations()
const today = () => dayjs().format('YYYY-MM-DD')

const form = reactive({
  amountKop: null as number | null,
  date: null as string | null,
  method: 'bank_transfer' as 'bank_transfer' | 'cash',
  reason: '',
})
const amountError = ref<string | null>(null)
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
watch(visible, (v) => {
  if (!v) return
  Object.assign(form, { amountKop: props.advanceKop, date: today(), method: 'bank_transfer', reason: '' })
  amountError.value = null
  submitted.value = false
  serverErrors.value = {}
  serverError.value = null
})

const header = computed(() => (props.mode === 'refund' ? 'Вернуть переплату' : 'Перенести переплату в залог'))
const errors = computed(() => {
  const e: Record<string, string> = {}
  if (amountError.value) e.amountKop = amountError.value
  else if (!form.amountKop || form.amountKop <= 0) e.amountKop = 'Сумма должна быть больше нуля'
  else if (form.amountKop > props.advanceKop) e.amountKop = 'Сумма больше аванса по платежу'
  if (!form.date) e.date = 'Укажите дату'
  else if (form.date > today()) e.date = 'Дата не может быть позже сегодняшней'
  if (props.mode === 'refund' && !form.reason.trim()) e.reason = 'Укажите причину'
  return { ...e, ...serverErrors.value }
})
const err = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(errors.value).length) return
  try {
    if (props.mode === 'refund') {
      await m.refund.mutateAsync({
        id: props.paymentId,
        body: { amountKop: form.amountKop!, refundedOn: form.date!, method: form.method, reason: form.reason.trim() },
      })
      toast.add({ severity: 'success', summary: 'Возврат переплаты оформлен', life: 3000 })
    } else {
      await m.moveToDeposit.mutateAsync({
        id: props.paymentId,
        body: { amountKop: form.amountKop!, opDate: form.date! },
      })
      toast.add({ severity: 'success', summary: 'Переплата перенесена в залог', life: 3000 })
    }
    visible.value = false
  } catch (e) {
    const p = parseProblem(e)
    serverErrors.value = { ...p.fieldErrors, ...(p.fieldErrors.refundedOn ? { date: p.fieldErrors.refundedOn } : {}) }
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    :header="header"
    :style="{ width: '480px' }"
    :breakpoints="{ '520px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="save">
      <p class="hint">Аванс по платежу: <MoneyText :kop="advanceKop" /></p>
      <div class="form-grid">
        <div class="field">
          <label for="ad-amount">Сумма *</label>
          <MoneyInput
            v-model="form.amountKop"
            input-id="ad-amount"
            :invalid="!!err('amountKop')"
            @error="(e) => (amountError = e)"
          />
          <small v-if="err('amountKop')" class="error">{{ err('amountKop') }}</small>
        </div>
        <div class="field">
          <label for="ad-date">{{ mode === 'refund' ? 'Дата возврата *' : 'Дата операции *' }}</label>
          <DateInput v-model="form.date" input-id="ad-date" :max-date="new Date()" :invalid="!!err('date')" />
          <small v-if="err('date')" class="error">{{ err('date') }}</small>
        </div>
      </div>
      <template v-if="mode === 'refund'">
        <div class="field">
          <label id="ad-method">Способ *</label>
          <SelectButton
            v-model="form.method"
            :options="options(refundMethodLabels)"
            option-label="label"
            option-value="value"
            :allow-empty="false"
            aria-labelledby="ad-method"
          />
        </div>
        <div class="field">
          <label for="ad-reason">Причина *</label>
          <Textarea id="ad-reason" v-model="form.reason" rows="2" auto-resize :invalid="!!err('reason')" fluid />
          <small v-if="err('reason')" class="error">{{ err('reason') }}</small>
        </div>
      </template>
      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button
          type="submit"
          :label="mode === 'refund' ? 'Вернуть' : 'Перенести'"
          :loading="m.refund.isPending.value || m.moveToDeposit.isPending.value"
        />
      </div>
    </form>
  </Dialog>
</template>
