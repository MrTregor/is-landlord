<script setup lang="ts">
// Вкладка «Договоры» карточки объекта (14 Экраны собственника, раздел 2.3, вкладка 2).
import { useRouter } from 'vue-router'
import { useContracts, type ContractListItem } from '@/api/queries/contracts'
import MoneyText from '@/components/MoneyText.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import { contractStatusLabels, contractStatusSeverity } from '@/utils/labels'

const props = defineProps<{ propertyId: string }>()
const router = useRouter()
const list = useContracts(() => ({ propertyId: props.propertyId, pageSize: 100, sort: '-startDate' }))
type Item = ContractListItem
</script>

<template>
  <DataTable
    :value="list.data.value?.items ?? []"
    :loading="list.isFetching.value"
    size="small"
    data-key="id"
    row-hover
    @row-click="(e) => router.push(`/contracts/${(e.data as Item).id}`)"
  >
    <template #empty>Договоров по объекту нет</template>
    <Column header="Номер">
      <template #body="{ data }">
        <RouterLink :to="`/contracts/${(data as Item).id}`" @click.stop>№ {{ (data as Item).number }}</RouterLink>
      </template>
    </Column>
    <Column header="Наниматель">
      <template #body="{ data }">{{ (data as Item).tenant.displayName }}</template>
    </Column>
    <Column header="Срок">
      <template #body="{ data }"
        >{{ formatDate((data as Item).startDate) }} – {{ formatDate((data as Item).actualEndDate) }}</template
      >
    </Column>
    <Column header="Статус">
      <template #body="{ data }">
        <StatusTag
          :label="contractStatusLabels[(data as Item).status]"
          :severity="contractStatusSeverity[(data as Item).status]"
        />
      </template>
    </Column>
    <Column header="Плата">
      <template #body="{ data }"><MoneyText :kop="(data as Item).rentKop" /></template>
    </Column>
  </DataTable>
</template>

<style scoped>
:deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
</style>
