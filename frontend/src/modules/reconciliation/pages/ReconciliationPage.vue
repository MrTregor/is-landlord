<script setup lang="ts">
// Сверка (14 Экраны собственника, раздел 7). Вкладка «Расхождения» — этап M6 (переплата, недоплата);
// «Выписки банка» и разбор неопознанных платежей — этап M9.
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useDiscrepancies, type DiscrepanciesQuery, type Discrepancy } from '@/api/queries/payments'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import PlannedTab from '@/components/PlannedTab.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import { discrepancyKindLabels, discrepancyResolutionLabels, discrepancyStatusLabels, options } from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'
import ResolveDialog from '../components/ResolveDialog.vue'

const route = useRoute()
const router = useRouter()
const tab = computed({
  get: () => (route.query.tab === 'statements' ? 'statements' : 'discrepancies'),
  set: (v: string | number) => void router.replace({ query: v === 'statements' ? { tab: 'statements' } : {} }),
})

const { state, set } = useListQuery({ status: 'open' })
const str = (k: string) => (state.value.values[k] as string | undefined) || undefined
const query = computed<DiscrepanciesQuery>(() => ({
  status: (str('status') as DiscrepanciesQuery['status']) ?? 'open',
  kind: str('kind') as DiscrepanciesQuery['kind'],
  page: state.value.page,
  pageSize: state.value.pageSize,
}))
const list = useDiscrepancies(query)
const openCount = useDiscrepancies({ status: 'open', pageSize: 1 })

const resolving = ref<Discrepancy | null>(null)
const resolveDialog = computed({
  get: () => resolving.value !== null,
  set: (v: boolean) => {
    if (!v) resolving.value = null
  },
})
const d = (x: unknown) => x as Discrepancy
</script>

<template>
  <div>
    <PageHeader title="Сверка" />
    <Tabs v-model:value="tab">
      <TabList>
        <Tab value="discrepancies">
          Расхождения
          <Badge
            v-if="openCount.data.value?.total"
            :value="openCount.data.value.total"
            severity="danger"
            class="badge"
          />
        </Tab>
        <Tab value="statements">Выписки банка</Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="discrepancies">
          <div class="filters">
            <SelectButton
              :model-value="query.status"
              :options="options(discrepancyStatusLabels)"
              option-label="label"
              option-value="value"
              :allow-empty="false"
              aria-label="Статус"
              @update:model-value="(v) => set({ status: v })"
            />
            <Select
              :model-value="state.values.kind"
              :options="options(discrepancyKindLabels)"
              option-label="label"
              option-value="value"
              placeholder="Вид"
              aria-label="Вид"
              show-clear
              @update:model-value="(v) => set({ kind: v ?? undefined })"
            />
          </div>
          <DataTable
            :value="list.data.value?.items ?? []"
            :loading="list.isFetching.value"
            lazy
            paginator
            :first="(state.page - 1) * state.pageSize"
            :rows="state.pageSize"
            :total-records="list.data.value?.total ?? 0"
            :rows-per-page-options="[25, 50, 100]"
            size="small"
            striped-rows
            data-key="id"
            @page="(e) => set({ page: e.page + 1, pageSize: e.rows }, false)"
          >
            <template #empty>Расхождений нет</template>
            <Column header="Обнаружено">
              <template #body="{ data }">{{ formatDateTime(d(data).detectedAt) }}</template>
            </Column>
            <Column header="Вид">
              <template #body="{ data }">{{ discrepancyKindLabels[d(data).kind] }}</template>
            </Column>
            <Column header="Сумма">
              <template #body="{ data }"><MoneyText :kop="d(data).amountKop" /></template>
            </Column>
            <Column header="Договор / плательщик">
              <template #body="{ data }">
                <template v-if="d(data).contract">
                  <RouterLink :to="`/contracts/${d(data).contract!.id}`">№ {{ d(data).contract!.number }}</RouterLink>
                  <div v-if="d(data).tenant" class="hint">{{ d(data).tenant!.displayName }}</div>
                </template>
                <span v-else-if="d(data).bankLine">{{ d(data).bankLine!.payerName ?? '—' }}</span>
                <span v-else>—</span>
              </template>
            </Column>
            <Column header="Подробности">
              <template #body="{ data }">
                <RouterLink v-if="d(data).paymentId" :to="`/payments/${d(data).paymentId}`">
                  платёж {{ d(data).receiptNumber }}
                </RouterLink>
                <template v-else-if="d(data).bankLine">
                  {{ formatDate(d(data).bankLine!.receivedOn) }} · {{ d(data).bankLine!.purpose }}
                </template>
              </template>
            </Column>
            <Column header="Статус">
              <template #body="{ data }">
                <template v-if="d(data).status === 'open'"><Tag value="Открыто" severity="warn" /></template>
                <template v-else>
                  <Tag :value="discrepancyResolutionLabels[d(data).resolution!]" severity="secondary" />
                  <div v-if="d(data).resolutionNote" class="hint">{{ d(data).resolutionNote }}</div>
                </template>
              </template>
            </Column>
            <Column header="">
              <template #body="{ data }">
                <Button
                  v-if="d(data).status === 'open'"
                  label="Разобрать"
                  size="small"
                  outlined
                  @click="resolving = d(data)"
                />
              </template>
            </Column>
          </DataTable>
        </TabPanel>
        <TabPanel value="statements"><PlannedTab stage="M9" /></TabPanel>
      </TabPanels>
    </Tabs>
    <ResolveDialog v-if="resolving" v-model:visible="resolveDialog" :discrepancy="resolving" />
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
.badge {
  margin-left: 0.5rem;
}
</style>
