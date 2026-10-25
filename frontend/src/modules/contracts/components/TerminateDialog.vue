<script setup lang="ts">
// Диалог «Расторгнуть» (14 Экраны собственника, раздел 4.5): последствия считаются на лету по 05 разд. 5.2.
import dayjs from 'dayjs'
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useContractMutations, type ContractDetail } from '@/api/queries/contracts'
import DateInput from '@/components/DateInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import { formatDate, fromIsoDate } from '@/utils/date'
import { options, terminationInitiatorLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { todayIso } from '@/utils/today'
import { lastMonthRent } from '../schemas'

const props = defineProps<{ contract: ContractDetail }>()
const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()
const m = useContractMutations(() => props.contract.id)

function defaultDay(): string {
  const today = todayIso()
  if (today < props.contract.startDate) return props.contract.startDate
  if (today > props.contract.endDate) return props.contract.endDate
  return today
}

const form = reactive({ terminationDate: defaultDay() as string | null, initiator: null as string | null, reason: '' })
const submitted = ref(false)
const error = ref<string | null>(null)

watch(visible, (v) => {
  if (v) {
    Object.assign(form, { terminationDate: defaultDay(), initiator: null, reason: '' })
    submitted.value = false
    error.value = null
  }
})

const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!form.terminationDate) e.terminationDate = 'Укажите последний день пользования'
  if (!form.initiator) e.initiator = 'Выберите инициатора'
  if (form.reason.trim().length < 5) e.reason = 'Причина — не короче 5 символов'
  return e
})

/** Плата за месяц последнего дня и число отменяемых будущих месяцев. */
const effect = computed(() => {
  if (!form.terminationDate) return null
  const last = form.terminationDate
  const rent = lastMonthRent(props.contract.currentTerms.rentKop, last)
  const end = dayjs(props.contract.endDate)
  const lastMonth = dayjs(last).startOf('month')
  const cancelled = Math.max(0, end.startOf('month').diff(lastMonth, 'month'))
  return { ...rent, cancelled }
})

async function submit() {
  submitted.value = true
  if (Object.keys(errors.value).length) return
  error.value = null
  try {
    await m.terminate.mutateAsync({
      terminationDate: form.terminationDate!,
      initiator: form.initiator as 'landlord' | 'tenant' | 'mutual',
      reason: form.reason.trim(),
    })
    visible.value = false
    toast.add({ severity: 'success', summary: 'Договор расторгнут', life: 3000 })
  } catch (err) {
    error.value = parseProblem(err).message
  }
}
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    header="Расторгнуть договор"
    :style="{ width: '560px' }"
    :breakpoints="{ '575px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="submit">
      <div class="field">
        <label for="t-last">Последний день пользования *</label>
        <DateInput
          v-model="form.terminationDate"
          input-id="t-last"
          :min-date="fromIsoDate(contract.startDate)"
          :max-date="fromIsoDate(contract.endDate)"
          :invalid="submitted && !!errors.terminationDate"
        />
        <small v-if="submitted && errors.terminationDate" class="error">{{ errors.terminationDate }}</small>
      </div>
      <div class="field">
        <label for="t-init">Инициатор *</label>
        <Select
          v-model="form.initiator"
          input-id="t-init"
          :options="options(terminationInitiatorLabels)"
          option-label="label"
          option-value="value"
          placeholder="Выберите"
          :invalid="submitted && !!errors.initiator"
          fluid
        />
        <small v-if="submitted && errors.initiator" class="error">{{ errors.initiator }}</small>
      </div>
      <div class="field">
        <label for="t-reason">Причина *</label>
        <Textarea
          id="t-reason"
          v-model="form.reason"
          rows="3"
          auto-resize
          :invalid="submitted && !!errors.reason"
          fluid
        />
        <small v-if="submitted && errors.reason" class="error">{{ errors.reason }}</small>
      </div>
      <div v-if="effect" class="effect">
        <div>
          Плата за последний месяц ({{ effect.days }} из {{ effect.daysInMonth }} дн. по
          {{ formatDate(form.terminationDate) }}):
          <MoneyText :kop="effect.amountKop" />
        </div>
        <div>Отменяемых будущих месяцев: {{ effect.cancelled }}</div>
      </div>
      <Message severity="warn" :closable="false">Действие необратимо.</Message>
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button type="submit" label="Расторгнуть" severity="danger" :loading="m.terminate.isPending.value" />
      </div>
    </form>
  </Dialog>
</template>

<style scoped>
.effect {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 0.75rem;
  border-radius: 6px;
  background: var(--p-content-hover-background);
}
</style>
