<script setup lang="ts">
// Таблица начислений с итогами (14 Экраны собственника, раздел 5): экран «Начисления», вкладки договора и объекта.
import { computed } from 'vue'
import { useCharges, type Charge, type ChargesQuery } from '@/api/queries/charges'
import MoneyText from '@/components/MoneyText.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import { chargeStatusLabels, chargeStatusSeverity, penaltyStateLabels } from '@/utils/labels'

const props = defineProps<{ query: ChargesQuery; showContract?: boolean }>()
const emit = defineEmits<{
  page: [page: number, pageSize: number]
  sort: [sort: string | undefined]
  open: [id: string]
}>()
const list = useCharges(() => props.query)

const sortField = computed(() => props.query.sort?.replace(/^-/, ''))
const sortOrder = computed(() => (props.query.sort?.startsWith('-') ? -1 : 1))
function onSort(e: { sortField?: unknown; sortOrder?: number | null }) {
  const f = typeof e.sortField === 'string' ? e.sortField : undefined
  emit('sort', f ? (e.sortOrder === -1 ? `-${f}` : f) : undefined)
}
const page = computed(() => props.query.page ?? 1)
const pageSize = computed(() => props.query.pageSize ?? 25)
</script>

<template>
  <div>
    <div class="summary">
      <div>Начислено: <MoneyText :kop="list.data.value?.summary.amountKop ?? 0" /></div>
      <div>Оплачено: <MoneyText :kop="list.data.value?.summary.paidKop ?? 0" /></div>
      <div>
        Остаток: <strong><MoneyText :kop="list.data.value?.summary.outstandingKop ?? 0" /></strong>
      </div>
    </div>
    <DataTable
      :value="list.data.value?.items ?? []"
      :loading="list.isFetching.value"
      lazy
      paginator
      :first="(page - 1) * pageSize"
      :rows="pageSize"
      :total-records="list.data.value?.total ?? 0"
      :rows-per-page-options="[25, 50, 100]"
      :sort-field="sortField"
      :sort-order="sortOrder"
      size="small"
      striped-rows
      data-key="id"
      row-hover
      :row-class="(r: Charge) => (r.status === 'cancelled' ? 'row--cancelled' : '')"
      @page="(e) => emit('page', e.page + 1, e.rows)"
      @sort="onSort"
      @row-click="(e) => emit('open', (e.data as Charge).id)"
    >
      <template #empty>Начислений нет</template>
      <Column field="dueDate" header="Срок оплаты" sortable>
        <template #body="{ data }">{{ formatDate((data as Charge).dueDate) || '—' }}</template>
      </Column>
      <Column field="periodMonth" header="Начисление" sortable>
        <template #body="{ data }">
          {{ (data as Charge).title }}
          <span class="hint origin">{{ (data as Charge).origin === 'auto' ? 'авто' : 'вручную' }}</span>
        </template>
      </Column>
      <template v-if="showContract">
        <Column header="Договор">
          <template #body="{ data }">
            <RouterLink :to="`/contracts/${(data as Charge).contract.id}`" @click.stop
              >№ {{ (data as Charge).contract.number }}</RouterLink
            >
          </template>
        </Column>
        <Column header="Объект">
          <template #body="{ data }">{{ (data as Charge).property.code }}</template>
        </Column>
        <Column header="Наниматель">
          <template #body="{ data }">{{ (data as Charge).tenant.displayName }}</template>
        </Column>
      </template>
      <Column field="amountKop" header="Сумма" sortable>
        <template #body="{ data }"><MoneyText :kop="(data as Charge).amountKop" /></template>
      </Column>
      <Column header="Оплачено">
        <template #body="{ data }"><MoneyText :kop="(data as Charge).paidKop" /></template>
      </Column>
      <Column header="Остаток">
        <template #body="{ data }"><MoneyText :kop="(data as Charge).outstandingKop" /></template>
      </Column>
      <Column header="Статус">
        <template #body="{ data }">
          <div class="status">
            <StatusTag
              :label="chargeStatusLabels[(data as Charge).status]"
              :severity="chargeStatusSeverity[(data as Charge).status]"
            />
            <Tag
              v-if="(data as Charge).isOverdue"
              :value="`Просрочено ${(data as Charge).overdueDays} дн.`"
              severity="danger"
            />
            <Tag
              v-if="(data as Charge).penaltyState"
              :value="penaltyStateLabels[(data as Charge).penaltyState!]"
              severity="info"
            />
          </div>
        </template>
      </Column>
    </DataTable>
  </div>
</template>

<style scoped>
.summary {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 2rem;
  margin-bottom: 0.75rem;
}
.origin {
  font-size: 0.8rem;
  margin-left: 0.25rem;
}
.status {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
:deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
:deep(.row--cancelled) {
  color: var(--p-text-muted-color);
  text-decoration: line-through;
}
</style>
