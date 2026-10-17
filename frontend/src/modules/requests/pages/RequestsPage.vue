<script setup lang="ts">
// Заявки (14 Экраны собственника, раздел 9.1): поиск, Статус (по умолчанию все, кроме «Закрыта»), Объект,
// Категория, Приоритет; «Новая заявка».
import { computed, ref } from 'vue'
import { useRequests, type RequestsQuery } from '@/api/queries/maintenance'
import { useProperties } from '@/api/queries/properties'
import type { components } from '@/api/schema'
import PageHeader from '@/components/PageHeader.vue'
import { options, requestCategoryLabels, requestPriorityLabels, requestStatusLabels } from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'
import { parseProblem } from '@/utils/problem'
import RequestDialog from '../components/RequestDialog.vue'
import RequestsTable from '../components/RequestsTable.vue'

type Status = components['schemas']['RequestStatus']
const { state, set, reset } = useListQuery({ status: ['new', 'in_progress', 'done'] }, ['status'])
const str = (k: string) => (state.value.values[k] as string | undefined) || undefined
const statuses = computed(() => (state.value.values.status as Status[] | undefined) ?? [])
const query = computed<RequestsQuery>(() => ({
  status: statuses.value,
  propertyId: str('propertyId'),
  category: str('category') as RequestsQuery['category'],
  priority: str('priority') as RequestsQuery['priority'],
  q: str('q'),
  page: state.value.page,
  pageSize: state.value.pageSize,
}))
const list = useRequests(query)
const properties = useProperties(() => ({ pageSize: 100 }))
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((p) => ({ value: p.id, label: p.code })),
)
const search = ref(str('q') ?? '')
const dialog = ref(false)

function applySearch() {
  set({ q: search.value.trim() || undefined })
}
function resetAll() {
  search.value = ''
  reset()
}
</script>

<template>
  <div>
    <PageHeader title="Заявки">
      <template #actions>
        <Button label="Новая заявка" icon="pi pi-plus" @click="dialog = true" />
      </template>
    </PageHeader>
    <div class="filters">
      <form role="search" @submit.prevent="applySearch">
        <IconField>
          <InputIcon class="pi pi-search" />
          <InputText v-model="search" placeholder="Номер или заголовок" aria-label="Поиск заявок" />
        </IconField>
      </form>
      <MultiSelect
        :model-value="statuses"
        :options="options(requestStatusLabels)"
        option-label="label"
        option-value="value"
        placeholder="Статус: все"
        aria-label="Статус"
        display="chip"
        @update:model-value="(v) => set({ status: v as string[] })"
      />
      <Select
        :model-value="state.values.propertyId"
        :options="propertyOptions"
        option-label="label"
        option-value="value"
        placeholder="Объект"
        aria-label="Объект"
        filter
        show-clear
        @update:model-value="(v) => set({ propertyId: v ?? undefined })"
      />
      <Select
        :model-value="state.values.category"
        :options="options(requestCategoryLabels)"
        option-label="label"
        option-value="value"
        placeholder="Категория"
        aria-label="Категория"
        show-clear
        @update:model-value="(v) => set({ category: v ?? undefined })"
      />
      <Select
        :model-value="state.values.priority"
        :options="options(requestPriorityLabels)"
        option-label="label"
        option-value="value"
        placeholder="Приоритет"
        aria-label="Приоритет"
        show-clear
        @update:model-value="(v) => set({ priority: v ?? undefined })"
      />
      <Button label="Сбросить" text @click="resetAll" />
    </div>
    <Message v-if="list.isError.value" severity="error" :closable="false">
      {{ parseProblem(list.error.value).message }}
    </Message>
    <RequestsTable
      :items="list.data.value?.items ?? []"
      :total="list.data.value?.total ?? 0"
      :loading="list.isFetching.value && !list.data.value"
      :first="(state.page - 1) * state.pageSize"
      :rows="state.pageSize"
      @page="(page, rows) => set({ page, pageSize: rows }, false)"
    />
    <RequestDialog v-model:visible="dialog" />
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}
</style>
