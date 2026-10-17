<script setup lang="ts">
// Таблица заявок (14 Экраны собственника, раздел 9.1): Номер, Создана, Объект, Заголовок, Категория, Приоритет,
// Статус, Исполнитель, Плановая дата, Автор.
import { useRouter } from 'vue-router'
import type { RequestItem } from '@/api/queries/maintenance'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import {
  requestCategoryLabels,
  requestPriorityLabels,
  requestStatusLabels,
  requestStatusSeverity,
} from '@/utils/labels'

defineProps<{
  items: RequestItem[]
  total: number
  loading?: boolean
  first?: number
  rows?: number
  hideProperty?: boolean
}>()
const emit = defineEmits<{ page: [page: number, rows: number] }>()
const router = useRouter()
const r = (x: unknown) => x as RequestItem
</script>

<template>
  <DataTable
    :value="items"
    :loading="loading"
    lazy
    paginator
    :first="first ?? 0"
    :rows="rows ?? 25"
    :total-records="total"
    :rows-per-page-options="[25, 50, 100]"
    size="small"
    striped-rows
    row-hover
    data-key="id"
    class="clickable"
    @page="(ev) => emit('page', ev.page + 1, ev.rows)"
    @row-click="(ev) => router.push(`/requests/${r(ev.data).id}`)"
  >
    <template #empty>Заявок нет</template>
    <Column header="Номер">
      <template #body="{ data }">
        <RouterLink :to="`/requests/${r(data).id}`" @click.stop>{{ r(data).number }}</RouterLink>
      </template>
    </Column>
    <Column header="Создана">
      <template #body="{ data }">{{ formatDateTime(r(data).createdAt) }}</template>
    </Column>
    <Column v-if="!hideProperty" header="Объект">
      <template #body="{ data }">{{ r(data).property.code }}</template>
    </Column>
    <Column header="Заголовок">
      <template #body="{ data }">{{ r(data).title }}</template>
    </Column>
    <Column header="Категория">
      <template #body="{ data }">{{ requestCategoryLabels[r(data).category] }}</template>
    </Column>
    <Column header="Приоритет">
      <template #body="{ data }">
        <span :class="{ high: r(data).priority === 'high' }">{{ requestPriorityLabels[r(data).priority] }}</span>
      </template>
    </Column>
    <Column header="Статус">
      <template #body="{ data }">
        <StatusTag :label="requestStatusLabels[r(data).status]" :severity="requestStatusSeverity[r(data).status]" />
      </template>
    </Column>
    <Column header="Исполнитель">
      <template #body="{ data }">{{ r(data).contractor ?? '—' }}</template>
    </Column>
    <Column header="Плановая дата">
      <template #body="{ data }">{{ formatDate(r(data).plannedDate) || '—' }}</template>
    </Column>
    <Column header="Автор">
      <template #body="{ data }">{{ r(data).authorRole === 'tenant' ? 'Наниматель' : 'Собственник' }}</template>
    </Column>
  </DataTable>
</template>

<style scoped>
.clickable :deep(tbody tr) {
  cursor: pointer;
}
.high {
  color: var(--p-red-600);
  font-weight: 600;
}
</style>
