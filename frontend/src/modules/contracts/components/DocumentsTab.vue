<script setup lang="ts">
// Вкладка «Документы» (14 Экраны собственника, раздел 4.3, вкладка 5): документ, номер, дата, видимость нанимателю,
// подписанный скан, PDF. Заменённые документы — серым с пометкой «заменён».
import { ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import type { ContractDetail } from '@/api/queries/contracts'
import { documentPdfUrl, useContractDocuments, useDocumentMutations, type DocumentDTO } from '@/api/queries/documents'
import { uploadFile } from '@/api/queries/properties'
import EmptyState from '@/components/EmptyState.vue'
import { formatDateTime } from '@/utils/date'
import { documentKindLabels } from '@/utils/labels'
import { openInNewTab } from '@/utils/pdf'
import { parseProblem } from '@/utils/problem'
import ReconciliationDialog from './ReconciliationDialog.vue'

const props = defineProps<{ contract: ContractDetail }>()
const toast = useToast()
const docs = useContractDocuments(() => props.contract.id)
const m = useDocumentMutations()

function fail(err: unknown) {
  toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
}

async function regenerate() {
  try {
    await openInNewTab(async () => documentPdfUrl((await m.contract.mutateAsync(props.contract.id)).id))
    toast.add({ severity: 'success', summary: 'Документ договора сформирован', life: 3000 })
  } catch (err) {
    fail(err)
  }
}

async function setVisible(d: DocumentDTO, visible: boolean) {
  try {
    await m.visibility.mutateAsync({ id: d.id, visibleToTenant: visible })
  } catch (err) {
    fail(err)
  }
}

const scanInput = ref<HTMLInputElement | null>(null)
const scanTarget = ref<DocumentDTO | null>(null)
function chooseScan(d: DocumentDTO) {
  scanTarget.value = d
  scanInput.value?.click()
}
async function uploadScan(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (!f || !scanTarget.value) return
  try {
    const file = await uploadFile(f, 'signed_scan')
    await m.signedScan.mutateAsync({ id: scanTarget.value.id, fileId: file.id })
    toast.add({ severity: 'success', summary: 'Скан прикреплён', life: 3000 })
  } catch (err) {
    fail(err)
  }
}

const reconDialog = ref(false)
const rowClass = (d: DocumentDTO) => (d.supersededBy ? 'superseded' : '')
</script>

<template>
  <div>
    <div class="toolbar">
      <Button
        label="Сформировать договор заново"
        icon="pi pi-file-pdf"
        outlined
        :loading="m.contract.isPending.value"
        @click="regenerate"
      />
      <Button
        v-if="contract.status !== 'draft'"
        label="Акт сверки"
        icon="pi pi-list"
        outlined
        @click="reconDialog = true"
      />
    </div>
    <Skeleton v-if="docs.isPending.value" height="6rem" />
    <Message v-else-if="docs.isError.value" severity="error" :closable="false">
      {{ parseProblem(docs.error.value).message }}
    </Message>
    <EmptyState v-else-if="!docs.data.value?.length" text="Документов пока нет" icon="pi pi-file" />
    <DataTable v-else :value="docs.data.value" data-key="id" :row-class="rowClass" size="small">
      <Column header="Документ">
        <template #body="{ data }">
          <div>{{ (data as DocumentDTO).title }}</div>
          <div class="hint">
            {{ documentKindLabels[(data as DocumentDTO).kind] }}
            <Tag v-if="(data as DocumentDTO).supersededBy" value="заменён" severity="secondary" />
          </div>
        </template>
      </Column>
      <Column header="Номер">
        <template #body="{ data }">{{ (data as DocumentDTO).number ?? '—' }}</template>
      </Column>
      <Column header="Дата">
        <template #body="{ data }">{{ formatDateTime((data as DocumentDTO).createdAt) }}</template>
      </Column>
      <Column header="Видим нанимателю">
        <template #body="{ data }">
          <ToggleSwitch
            :model-value="(data as DocumentDTO).visibleToTenant"
            :aria-label="`Видим нанимателю: ${(data as DocumentDTO).title}`"
            @update:model-value="(v: boolean) => setVisible(data as DocumentDTO, v)"
          />
        </template>
      </Column>
      <Column header="Подписанный скан">
        <template #body="{ data }">
          <a v-if="(data as DocumentDTO).signedScanUrl" :href="(data as DocumentDTO).signedScanUrl!" target="_blank"
            >Открыть</a
          >
          <Button
            :label="(data as DocumentDTO).signedScanUrl ? 'Заменить' : 'Загрузить'"
            size="small"
            text
            @click="chooseScan(data as DocumentDTO)"
          />
        </template>
      </Column>
      <Column header="">
        <template #body="{ data }">
          <a :href="(data as DocumentDTO).fileUrl" target="_blank" class="p-button p-button-text p-button-sm">
            <i class="pi pi-file-pdf" aria-hidden="true" />&nbsp;Открыть PDF
          </a>
        </template>
      </Column>
    </DataTable>
    <input
      ref="scanInput"
      type="file"
      accept="application/pdf,image/jpeg,image/png"
      class="hidden-input"
      @change="uploadScan"
    />
    <ReconciliationDialog v-model:visible="reconDialog" :contract="contract" />
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}
.hint {
  color: var(--p-text-muted-color);
  font-size: 0.85rem;
}
:deep(.superseded) {
  color: var(--p-text-muted-color);
}
.hidden-input {
  display: none;
}
</style>
