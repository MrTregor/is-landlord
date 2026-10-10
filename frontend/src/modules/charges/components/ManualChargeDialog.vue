<script setup lang="ts">
// Диалог «Ручное начисление» (14 Экраны собственника, раздел 5): договор, вид, название, сумма, срок, причина.
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useChargeMutations } from '@/api/queries/charges'
import { useContracts } from '@/api/queries/contracts'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import { manualChargeKindLabels, options } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'

const props = defineProps<{ contractId?: string }>()
const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()
const m = useChargeMutations()
const contracts = useContracts(() => ({ status: ['active', 'extended', 'terminated', 'finished'], pageSize: 100 }))
const contractOptions = computed(() =>
  (contracts.data.value?.items ?? []).map((c) => ({
    value: c.id,
    label: `№ ${c.number} — ${c.property.code}, ${c.tenant.displayName}`,
  })),
)

const form = reactive({
  contractId: null as string | null,
  kind: 'damage' as 'damage' | 'other',
  title: '',
  amountKop: null as number | null,
  dueDate: null as string | null,
  reason: '',
})
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
watch(visible, (v) => {
  if (!v) return
  Object.assign(form, {
    contractId: props.contractId ?? null,
    kind: 'damage',
    title: '',
    amountKop: null,
    dueDate: null,
    reason: '',
  })
  submitted.value = false
  serverErrors.value = {}
  serverError.value = null
})
const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!form.contractId) e.contractId = 'Выберите договор'
  if (!form.title.trim()) e.title = 'Укажите название'
  if (!form.amountKop || form.amountKop <= 0) e.amountKop = 'Сумма должна быть больше нуля'
  if (!form.dueDate) e.dueDate = 'Укажите срок оплаты'
  if (form.reason.trim().length < 5) e.reason = 'Причина — не короче 5 символов'
  return { ...e, ...serverErrors.value }
})
const err = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  if (Object.keys(errors.value).length) return
  try {
    await m.create.mutateAsync({
      contractId: form.contractId!,
      kind: form.kind,
      title: form.title.trim(),
      amountKop: form.amountKop!,
      dueDate: form.dueDate!,
      reason: form.reason.trim(),
    })
    visible.value = false
    toast.add({ severity: 'success', summary: 'Начисление создано', life: 3000 })
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
    header="Ручное начисление"
    :style="{ width: '540px' }"
    :breakpoints="{ '580px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="save">
      <div v-if="!contractId" class="field">
        <label for="mc-contract">Договор *</label>
        <Select
          v-model="form.contractId"
          input-id="mc-contract"
          :options="contractOptions"
          option-label="label"
          option-value="value"
          filter
          :invalid="!!err('contractId')"
          fluid
        />
        <small v-if="err('contractId')" class="error">{{ err('contractId') }}</small>
      </div>
      <div class="field">
        <label id="mc-kind">Вид *</label>
        <SelectButton
          v-model="form.kind"
          :options="options(manualChargeKindLabels)"
          option-label="label"
          option-value="value"
          :allow-empty="false"
          aria-labelledby="mc-kind"
        />
      </div>
      <div class="field">
        <label for="mc-title">Название *</label>
        <InputText id="mc-title" v-model="form.title" :invalid="!!err('title')" fluid />
        <small v-if="err('title')" class="error">{{ err('title') }}</small>
      </div>
      <div class="form-grid">
        <div class="field">
          <label for="mc-amount">Сумма *</label>
          <MoneyInput v-model="form.amountKop" input-id="mc-amount" :invalid="!!err('amountKop')" />
          <small v-if="err('amountKop')" class="error">{{ err('amountKop') }}</small>
        </div>
        <div class="field">
          <label for="mc-due">Срок оплаты *</label>
          <DateInput v-model="form.dueDate" input-id="mc-due" :invalid="!!err('dueDate')" />
          <small v-if="err('dueDate')" class="error">{{ err('dueDate') }}</small>
        </div>
      </div>
      <div class="field">
        <label for="mc-reason">Причина *</label>
        <Textarea id="mc-reason" v-model="form.reason" rows="2" auto-resize :invalid="!!err('reason')" fluid />
        <small v-if="err('reason')" class="error">{{ err('reason') }}</small>
      </div>
      <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button type="submit" label="Создать" :loading="m.create.isPending.value" />
      </div>
    </form>
  </Dialog>
</template>
