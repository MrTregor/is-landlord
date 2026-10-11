<script setup lang="ts">
// Настройки → Шаблоны документов (14 Экраны собственника, раздел 16, вкладка 5): таблица видов.
import { useRouter } from 'vue-router'
import { useDocumentTemplates } from '@/api/queries/documents'
import type { components } from '@/api/schema'
import { formatDateTime } from '@/utils/date'
import { parseProblem } from '@/utils/problem'

type Item = components['schemas']['DocumentTemplateListItem']
const router = useRouter()
const list = useDocumentTemplates()
</script>

<template>
  <div>
    <Skeleton v-if="list.isPending.value" height="10rem" />
    <Message v-else-if="list.isError.value" severity="error" :closable="false">
      {{ parseProblem(list.error.value).message }}
    </Message>
    <DataTable
      v-else
      :value="list.data.value"
      data-key="kind"
      size="small"
      row-hover
      class="clickable"
      @row-click="(e) => router.push(`/settings/document-templates/${(e.data as Item).kind}`)"
    >
      <Column header="Название">
        <template #body="{ data }">
          <RouterLink :to="`/settings/document-templates/${(data as Item).kind}`">{{ (data as Item).name }}</RouterLink>
        </template>
      </Column>
      <Column header="Версия">
        <template #body="{ data }">{{ (data as Item).version }}</template>
      </Column>
      <Column header="Изменён">
        <template #body="{ data }">
          {{ formatDateTime((data as Item).createdAt) }}
          <span v-if="(data as Item).createdBy" class="hint">· {{ (data as Item).createdBy }}</span>
          <span v-else class="hint">· стандартный</span>
        </template>
      </Column>
    </DataTable>
  </div>
</template>

<style scoped>
.hint {
  color: var(--p-text-muted-color);
}
.clickable :deep(tbody tr) {
  cursor: pointer;
}
</style>
