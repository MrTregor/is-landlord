<script setup lang="ts">
// Акт сверки за период (09 Документы и шаблоны, разд. 4.6): по умолчанию — с начала договора (не раньше 1 января
// текущего года) по сегодня или по дату окончания.
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import type { ContractDetail } from '@/api/queries/contracts'
import { documentPdfUrl, useDocumentMutations } from '@/api/queries/documents'
import DateInput from '@/components/DateInput.vue'
import { openInNewTab } from '@/utils/pdf'
import { parseProblem } from '@/utils/problem'
import { todayIso } from '@/utils/today'

const props = defineProps<{ contract: ContractDetail }>()
const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()
const m = useDocumentMutations()

function defaults() {
  const today = todayIso()
  const yearStart = `${today.slice(0, 4)}-01-01`
  const from = props.contract.startDate > yearStart ? props.contract.startDate : yearStart
  const to = props.contract.actualEndDate < today ? props.contract.actualEndDate : today
  return { from: from as string | null, to: (to < from ? from : to) as string | null }
}

const form = reactive(defaults())
const submitted = ref(false)
const error = ref<string | null>(null)
watch(visible, (v) => {
  if (v) {
    Object.assign(form, defaults())
    submitted.value = false
    error.value = null
  }
})

const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!form.from) e.from = 'Укажите начало периода'
  if (!form.to) e.to = 'Укажите конец периода'
  else if (form.from && form.to < form.from) e.to = 'Конец периода раньше начала'
  return e
})

async function submit() {
  submitted.value = true
  if (Object.keys(errors.value).length) return
  error.value = null
  try {
    await openInNewTab(async () => {
      const d = await m.reconciliation.mutateAsync({ contractId: props.contract.id, from: form.from!, to: form.to! })
      return documentPdfUrl(d.id)
    })
    visible.value = false
    toast.add({ severity: 'success', summary: 'Акт сверки сформирован', life: 3000 })
  } catch (err) {
    error.value = parseProblem(err).message
  }
}
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    header="Акт сверки"
    :style="{ width: '440px' }"
    :breakpoints="{ '575px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="submit">
      <div class="field">
        <label for="r-from">С *</label>
        <DateInput v-model="form.from" input-id="r-from" :invalid="submitted && !!errors.from" />
        <small v-if="submitted && errors.from" class="error">{{ errors.from }}</small>
      </div>
      <div class="field">
        <label for="r-to">По *</label>
        <DateInput v-model="form.to" input-id="r-to" :invalid="submitted && !!errors.to" />
        <small v-if="submitted && errors.to" class="error">{{ errors.to }}</small>
      </div>
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button type="submit" label="Сформировать" icon="pi pi-file-pdf" :loading="m.reconciliation.isPending.value" />
      </div>
    </form>
  </Dialog>
</template>
