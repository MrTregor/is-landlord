<script setup lang="ts">
// Блок «Чек» карточки платежа (14 Экраны собственника, раздел 6.3): статус, реквизиты, ссылка на чек,
// «Повторить» при ошибке, ссылка на чек НПД.
import { computed, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useFiscalMutations, type FiscalReceipt } from '@/api/queries/fiscal'
import StatusTag from '@/components/StatusTag.vue'
import { formatDateTime } from '@/utils/date'
import { fiscalStatusLabels, fiscalStatusSeverity } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import ManualReceiptDialog from './ManualReceiptDialog.vue'

const props = defineProps<{ receipt: FiscalReceipt }>()
const toast = useToast()
const m = useFiscalMutations()
const manual = ref(false)
const r = computed(() => props.receipt)
const link = computed(() => r.value.ofdReceiptUrl ?? r.value.npdReceiptUrl)

async function retry() {
  try {
    await m.retry.mutateAsync(r.value.id)
    toast.add({ severity: 'info', summary: 'Чек отправлен повторно', life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(e).message, life: 6000 })
  }
}
</script>

<template>
  <div>
    <StatusTag :label="fiscalStatusLabels[r.status]" :severity="fiscalStatusSeverity[r.status]" />
    <dl v-if="r.status === 'done' && r.fiscalDocumentNumber" class="props">
      <dt>Чек</dt>
      <dd>№ {{ r.fiscalReceiptNumber }}, смена {{ r.shiftNumber }}</dd>
      <dt>Дата и время</dt>
      <dd>{{ r.receiptDatetime ? formatDateTime(r.receiptDatetime) : '—' }}</dd>
      <dt>ФН</dt>
      <dd>{{ r.fnNumber }}</dd>
      <dt>ФД / ФП</dt>
      <dd>{{ r.fiscalDocumentNumber }} / {{ r.fiscalDocumentAttribute }}</dd>
    </dl>
    <p v-if="r.status === 'fail'" class="error">{{ r.errorText }}</p>
    <p v-if="r.status === 'manual_required'" class="hint">Сформируйте чек в «Мой налог» и укажите ссылку на него.</p>
    <div class="actions actions--start">
      <Button
        v-if="link"
        as="a"
        :href="link"
        target="_blank"
        rel="noopener"
        label="Открыть чек"
        icon="pi pi-external-link"
        size="small"
        outlined
      />
      <Button
        v-if="r.status === 'fail'"
        label="Повторить"
        icon="pi pi-refresh"
        size="small"
        :loading="m.retry.isPending.value"
        @click="retry"
      />
      <Button
        v-if="r.status === 'manual_required'"
        label="Указать ссылку на чек НПД"
        icon="pi pi-link"
        size="small"
        @click="manual = true"
      />
    </div>
    <ManualReceiptDialog v-model:visible="manual" :receipt-id="r.id" />
  </div>
</template>

<style scoped>
.props {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.25rem 1rem;
  margin: 0.75rem 0 0;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
}
.error {
  color: var(--p-red-600);
  margin: 0.5rem 0 0;
}
.actions--start {
  justify-content: flex-start;
  margin-top: 0.75rem;
}
</style>
