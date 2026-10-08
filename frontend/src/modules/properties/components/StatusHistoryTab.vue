<script setup lang="ts">
// История статусов объекта (14 Экраны собственника, раздел 2.3, вкладка 8).
import { fetchStatusHistory, usePropertySub } from '@/api/queries/properties'
import type { components } from '@/api/schema'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import { propertyStatusLabels, propertyStatusSeverity } from '@/utils/labels'

type Item = components['schemas']['PropertyStatusHistoryItem']
const props = defineProps<{ propertyId: string }>()
const history = usePropertySub(() => props.propertyId, 'status-history', fetchStatusHistory)
</script>

<template>
  <DataTable
    :value="history.data.value ?? []"
    :loading="history.isFetching.value"
    size="small"
    striped-rows
    data-key="id"
  >
    <template #empty>Записей нет</template>
    <Column header="Статус">
      <template #body="{ data }">
        <StatusTag
          :label="propertyStatusLabels[(data as Item).status]"
          :severity="propertyStatusSeverity[(data as Item).status]"
        />
      </template>
    </Column>
    <Column header="С">
      <template #body="{ data }">{{ formatDate((data as Item).validFrom) }}</template>
    </Column>
    <Column header="По">
      <template #body="{ data }">{{
        (data as Item).validTo ? formatDate((data as Item).validTo) : 'по настоящее время'
      }}</template>
    </Column>
    <Column header="Договор">
      <template #body="{ data }">
        <RouterLink v-if="(data as Item).contractId" :to="`/contracts/${(data as Item).contractId}`">
          № {{ (data as Item).contractNumber }}
        </RouterLink>
        <span v-else class="hint">—</span>
      </template>
    </Column>
    <Column header="Причина">
      <template #body="{ data }">{{ (data as Item).reason ?? '—' }}</template>
    </Column>
  </DataTable>
</template>
