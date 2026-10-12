<script setup lang="ts">
// Таблица платежей (14 Экраны собственника, раздел 6.1): экран «Платежи» и вкладка «Платежи» договора.
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { usePayments, type Payment, type PaymentsQuery } from '@/api/queries/payments'
import MoneyText from '@/components/MoneyText.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import {
  fiscalStatusLabels,
  fiscalStatusSeverity,
  paymentMethodLabels,
  paymentPurposeLabels,
  paymentStatusLabels,
  paymentStatusSeverity,
} from '@/utils/labels'

const props = defineProps<{ query: PaymentsQuery; showContract?: boolean }>()
const emit = defineEmits<{ page: [page: number, pageSize: number]; sort: [sort: string | undefined] }>()
const router = useRouter()
const list = usePayments(() => props.query)

const sortField = computed(() => props.query.sort?.replace(/^-/, ''))
const sortOrder = computed(() => (props.query.sort?.startsWith('-') ? -1 : 1))
function onSort(e: { sortField?: unknown; sortOrder?: number | null }) {
  const f = typeof e.sortField === 'string' ? e.sortField : undefined
  emit('sort', f ? (e.sortOrder === -1 ? `-${f}` : f) : undefined)
}
const page = computed(() => props.query.page ?? 1)
const pageSize = computed(() => props.query.pageSize ?? 25)
const p = (d: unknown) => d as Payment
</script>

<template>
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
    :row-class="(r: Payment) => (r.status === 'cancelled' ? 'row--cancelled' : '')"
    @page="(e) => emit('page', e.page + 1, e.rows)"
    @sort="onSort"
    @row-click="(e) => router.push(`/payments/${p(e.data).id}`)"
  >
    <template #empty>Платежей нет</template>
    <Column field="receivedOn" header="Дата поступления" sortable>
      <template #body="{ data }">{{ formatDate(p(data).receivedOn) || '—' }}</template>
    </Column>
    <Column header="Квитанция">
      <template #body="{ data }">{{ p(data).receiptNumber ?? '—' }}</template>
    </Column>
    <template v-if="showContract">
      <Column header="Договор">
        <template #body="{ data }">
          <RouterLink :to="`/contracts/${p(data).contract.id}`" @click.stop>№ {{ p(data).contract.number }}</RouterLink>
        </template>
      </Column>
      <Column header="Наниматель">
        <template #body="{ data }">{{ p(data).tenant.displayName }}</template>
      </Column>
      <Column header="Объект">
        <template #body="{ data }">{{ p(data).property.code }}</template>
      </Column>
    </template>
    <Column header="Назначение">
      <template #body="{ data }">{{ paymentPurposeLabels[p(data).purpose] }}</template>
    </Column>
    <Column field="amountKop" header="Сумма" sortable>
      <template #body="{ data }"><MoneyText :kop="p(data).amountKop" /></template>
    </Column>
    <Column header="Распределено">
      <template #body="{ data }"><MoneyText :kop="p(data).allocatedKop" /></template>
    </Column>
    <Column header="Аванс">
      <template #body="{ data }"><MoneyText :kop="p(data).advanceKop" /></template>
    </Column>
    <Column header="Способ">
      <template #body="{ data }">{{ paymentMethodLabels[p(data).method] }}</template>
    </Column>
    <Column header="Статус">
      <template #body="{ data }">
        <StatusTag :label="paymentStatusLabels[p(data).status]" :severity="paymentStatusSeverity[p(data).status]" />
      </template>
    </Column>
    <Column header="Чек">
      <template #body="{ data }">
        <StatusTag
          v-if="p(data).fiscalStatus"
          :label="fiscalStatusLabels[p(data).fiscalStatus!]"
          :severity="fiscalStatusSeverity[p(data).fiscalStatus!]"
        />
        <span v-else class="hint">—</span>
      </template>
    </Column>
  </DataTable>
</template>

<style scoped>
:deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
:deep(.row--cancelled) {
  color: var(--p-text-muted-color);
  text-decoration: line-through;
}
</style>
