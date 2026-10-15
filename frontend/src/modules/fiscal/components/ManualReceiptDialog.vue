<script setup lang="ts">
// «Указать ссылку на чек НПД» (14 Экраны собственника, раздел 13): ссылка на чек из «Мой налог».
import { computed, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useFiscalMutations } from '@/api/queries/fiscal'
import { parseProblem } from '@/utils/problem'

const props = defineProps<{ receiptId: string }>()
const visible = defineModel<boolean>('visible', { required: true })
const toast = useToast()
const m = useFiscalMutations()

const PREFIX = 'https://lknpd.nalog.ru/'
const link = ref('')
const submitted = ref(false)
const serverError = ref<string | null>(null)
watch(visible, (v) => {
  if (!v) return
  link.value = ''
  submitted.value = false
  serverError.value = null
})

const error = computed(() => {
  const v = link.value.trim()
  if (!v) return 'Укажите ссылку'
  if (!v.startsWith(PREFIX) || v.length === PREFIX.length) return `Ссылка должна начинаться с ${PREFIX}`
  return serverError.value
})

async function save() {
  submitted.value = true
  serverError.value = null
  if (error.value) return
  try {
    await m.manual.mutateAsync({ id: props.receiptId, npdReceiptUrl: link.value.trim() })
    toast.add({ severity: 'success', summary: 'Чек НПД сохранён', life: 3000 })
    visible.value = false
  } catch (e) {
    const p = parseProblem(e)
    serverError.value = p.fieldErrors.npdReceiptUrl ?? p.message
  }
}
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    header="Ссылка на чек НПД"
    :style="{ width: '520px' }"
    :breakpoints="{ '560px': '100vw' }"
  >
    <form class="form" novalidate @submit.prevent="save">
      <p class="hint">Сформируйте чек в приложении «Мой налог» и вставьте ссылку на него.</p>
      <div class="field">
        <label for="npd-link">Ссылка *</label>
        <InputText
          id="npd-link"
          v-model="link"
          :placeholder="`${PREFIX}…`"
          :invalid="submitted && !!error"
          fluid
          autofocus
        />
        <small v-if="submitted && error" class="p-error">{{ error }}</small>
      </div>
      <div class="actions">
        <Button type="button" label="Отмена" outlined @click="visible = false" />
        <Button type="submit" label="Сохранить" :loading="m.manual.isPending.value" />
      </div>
    </form>
  </Dialog>
</template>
