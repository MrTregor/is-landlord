<script setup lang="ts">
// Затраты (14 Экраны собственника, раздел 10): фильтры «Объект», «Категория», «Период оплаты», таблица с итогом,
// «Добавить затрату».
import { computed, ref } from 'vue'
import { useExpenses, type ExpensesQuery } from '@/api/queries/maintenance'
import { useProperties } from '@/api/queries/properties'
import DateInput from '@/components/DateInput.vue'
import { xlsxUrl } from '@/api/queries/reports'
import PageHeader from '@/components/PageHeader.vue'
import { expenseCategoryLabels, options } from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'
import { parseProblem } from '@/utils/problem'
import ExpenseDialog from '../components/ExpenseDialog.vue'
import ExpensesTable from '../components/ExpensesTable.vue'

const { state, set, reset } = useListQuery()
const str = (k: string) => (state.value.values[k] as string | undefined) || undefined
const query = computed<ExpensesQuery>(() => ({
  propertyId: str('propertyId'),
  category: str('category') as ExpensesQuery['category'],
  from: str('from'),
  to: str('to'),
  page: state.value.page,
  pageSize: state.value.pageSize,
}))
/** Выгрузка XLSX с теми же фильтрами, без страницы (11 разд. 8). */
const exportUrl = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { page, pageSize, ...rest } = query.value as Record<string, unknown>
  return xlsxUrl('/expenses.xlsx', rest as Parameters<typeof xlsxUrl>[1])
})
const list = useExpenses(query)
const properties = useProperties(() => ({ pageSize: 100 }))
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((p) => ({ value: p.id, label: p.code })),
)
const dialog = ref(false)
</script>

<template>
  <div>
    <PageHeader title="Затраты">
      <template #actions>
        <Button as="a" :href="exportUrl" download label="Выгрузить в Excel" icon="pi pi-file-excel" outlined />
        <Button label="Добавить затрату" icon="pi pi-plus" @click="dialog = true" />
      </template>
    </PageHeader>
    <div class="filters">
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
        :options="options(expenseCategoryLabels)"
        option-label="label"
        option-value="value"
        placeholder="Категория"
        aria-label="Категория"
        show-clear
        @update:model-value="(v) => set({ category: v ?? undefined })"
      />
      <span class="hint">Период оплаты:</span>
      <div class="date">
        <DateInput
          :model-value="str('from') ?? null"
          placeholder="с"
          @update:model-value="(v) => set({ from: v ?? undefined })"
        />
      </div>
      <div class="date">
        <DateInput
          :model-value="str('to') ?? null"
          placeholder="по"
          @update:model-value="(v) => set({ to: v ?? undefined })"
        />
      </div>
      <Button label="Сбросить" text @click="reset()" />
    </div>
    <Message v-if="list.isError.value" severity="error" :closable="false">
      {{ parseProblem(list.error.value).message }}
    </Message>
    <ExpensesTable
      :items="list.data.value?.items ?? []"
      :total="list.data.value?.total ?? 0"
      :total-kop="list.data.value?.summary.amountKop ?? 0"
      :loading="list.isFetching.value && !list.data.value"
      :first="(state.page - 1) * state.pageSize"
      :rows="state.pageSize"
      paginator
      @page="(page, rows) => set({ page, pageSize: rows }, false)"
    />
    <ExpenseDialog v-model:visible="dialog" />
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
.date {
  width: 11rem;
}
.hint {
  color: var(--p-text-muted-color);
}
</style>
