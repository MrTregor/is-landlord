<script setup lang="ts">
// Вкладка «Журнал» карточки договора: записи по договору, допсоглашениям и актам.
import { ref } from 'vue'
import { useContractTimeline } from '@/api/queries/contracts'
import type { components } from '@/api/schema'
import AuditDiff from '@/components/AuditDiff.vue'
import { formatDateTime } from '@/utils/date'

type Entry = components['schemas']['AuditEntryDTO']
const props = defineProps<{ contractId: string }>()
const page = ref(1)
const timeline = useContractTimeline(() => props.contractId, page)
const expanded = ref<Record<string, boolean>>({})
</script>

<template>
  <DataTable
    v-model:expanded-rows="expanded"
    :value="timeline.data.value?.items ?? []"
    :loading="timeline.isFetching.value"
    lazy
    paginator
    :rows="25"
    :first="(page - 1) * 25"
    :total-records="timeline.data.value?.total ?? 0"
    size="small"
    data-key="id"
    @page="(e) => (page = e.page + 1)"
  >
    <template #empty>Записей нет</template>
    <Column expander :style="{ width: '3rem' }" />
    <Column header="Дата и время">
      <template #body="{ data }">{{ formatDateTime((data as Entry).occurredAt) }}</template>
    </Column>
    <Column header="Пользователь">
      <template #body="{ data }">{{ (data as Entry).actorName ?? 'Система' }}</template>
    </Column>
    <Column field="actionTitle" header="Действие" />
    <Column field="summary" header="Описание" />
    <template #expansion="{ data }">
      <AuditDiff :before="(data as Entry).before" :after="(data as Entry).after" />
    </template>
  </DataTable>
</template>
