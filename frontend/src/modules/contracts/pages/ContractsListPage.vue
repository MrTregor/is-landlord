<script setup lang="ts">
// Договоры (14 Экраны собственника, раздел 4.1).
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useContracts, type ContractListItem, type ContractsQuery } from '@/api/queries/contracts'
import { useProperties } from '@/api/queries/properties'
import EmptyState from '@/components/EmptyState.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import { contractStatusLabels, contractStatusSeverity, legalBasisShort, options } from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'

type Item = ContractListItem
const router = useRouter()
const { state, set, reset } = useListQuery({ status: ['draft', 'active', 'extended'] }, ['status'])

const params = computed<ContractsQuery>(() => {
  const v = state.value.values
  return {
    q: (v.q as string | undefined) || undefined,
    status: (v.status as ContractsQuery['status'])?.length ? (v.status as ContractsQuery['status']) : undefined,
    propertyId: (v.propertyId as string | undefined) || undefined,
    endingWithinDays: v.ending === '1' ? 90 : undefined,
    sort: (state.value.sort as ContractsQuery['sort']) ?? '-number',
    page: state.value.page,
    pageSize: state.value.pageSize,
  }
})
const list = useContracts(params)
const properties = useProperties({ pageSize: 100 })
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((p) => ({ label: `${p.code} — ${p.address}`, value: p.id })),
)

const search = ref((state.value.values.q as string | undefined) ?? '')
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => set({ q: v.trim() || undefined }), 300)
})

const sortField = computed(() => state.value.sort?.replace(/^-/, '') ?? 'number')
const sortOrder = computed(() => (state.value.sort ? (state.value.sort.startsWith('-') ? -1 : 1) : -1))
function onSort(e: { sortField?: unknown; sortOrder?: number | null }) {
  const f = typeof e.sortField === 'string' ? e.sortField : undefined
  set({ sort: f ? (e.sortOrder === -1 ? `-${f}` : f) : undefined }, false)
}
const filtersEmpty = computed(
  () => !state.value.values.q && !state.value.values.propertyId && !state.value.values.ending,
)
</script>

<template>
  <div>
    <PageHeader title="Договоры">
      <template #actions>
        <Button label="Новый договор" icon="pi pi-plus" @click="router.push('/contracts/new')" />
      </template>
    </PageHeader>

    <div class="filters">
      <IconField>
        <InputIcon class="pi pi-search" />
        <InputText v-model="search" placeholder="Номер, наниматель, объект" aria-label="Поиск" />
      </IconField>
      <MultiSelect
        :model-value="(state.values.status as string[] | undefined) ?? []"
        :options="options(contractStatusLabels)"
        option-label="label"
        option-value="value"
        placeholder="Статус"
        aria-label="Статус"
        display="chip"
        @update:model-value="(v: string[]) => set({ status: v })"
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
        class="filters__property"
        @update:model-value="(v) => set({ propertyId: v ?? undefined })"
      />
      <div class="field field--check">
        <Checkbox
          input-id="c-ending"
          binary
          :model-value="state.values.ending === '1'"
          @update:model-value="(v: boolean) => set({ ending: v ? '1' : undefined })"
        />
        <label for="c-ending">Заканчиваются в течение 90 дней</label>
      </div>
      <Button
        label="Сбросить"
        text
        @click="
          () => {
            search = ''
            reset()
          }
        "
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
      :sort-field="sortField"
      :sort-order="sortOrder"
      striped-rows
      size="small"
      data-key="id"
      row-hover
      @page="(e) => set({ page: e.page + 1, pageSize: e.rows }, false)"
      @sort="onSort"
      @row-click="(e) => router.push(`/contracts/${(e.data as Item).id}`)"
    >
      <template #empty>
        <EmptyState
          v-if="filtersEmpty && !list.isFetching.value"
          text="Договоров пока нет. Создайте первый договор"
          action-label="Новый договор"
          icon="pi pi-file"
          @action="router.push('/contracts/new')"
        />
        <span v-else>Ничего не найдено</span>
      </template>
      <Column field="number" header="Номер" sortable>
        <template #body="{ data }">
          <RouterLink :to="`/contracts/${(data as Item).id}`" @click.stop>№ {{ (data as Item).number }}</RouterLink>
        </template>
      </Column>
      <Column header="Вид">
        <template #body="{ data }">
          {{ legalBasisShort[(data as Item).legalBasis] }}
          <Tag v-if="(data as Item).isShortTerm" value="Краткосрочный" severity="secondary" class="ml" />
        </template>
      </Column>
      <Column header="Объект">
        <template #body="{ data }">
          <RouterLink :to="`/properties/${(data as Item).property.id}`" @click.stop>{{
            (data as Item).property.code
          }}</RouterLink>
        </template>
      </Column>
      <Column header="Наниматель">
        <template #body="{ data }">
          <RouterLink :to="`/tenants/${(data as Item).tenant.id}`" @click.stop>{{
            (data as Item).tenant.displayName
          }}</RouterLink>
        </template>
      </Column>
      <Column field="startDate" header="Срок" sortable>
        <template #body="{ data }">
          {{ formatDate((data as Item).startDate) }} – {{ formatDate((data as Item).actualEndDate) }}
        </template>
      </Column>
      <Column header="Плата">
        <template #body="{ data }"><MoneyText :kop="(data as Item).rentKop" /></template>
      </Column>
      <Column header="Статус">
        <template #body="{ data }">
          <StatusTag
            :label="contractStatusLabels[(data as Item).status]"
            :severity="contractStatusSeverity[(data as Item).status]"
          />
        </template>
      </Column>
      <Column header="Долг">
        <template #body="{ data }"><MoneyText :kop="(data as Item).debtKop" /></template>
      </Column>
      <Column header="Залог (остаток)">
        <template #body="{ data }"><MoneyText :kop="(data as Item).depositBalanceKop" /></template>
      </Column>
    </DataTable>
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 1rem;
}
.filters__property {
  min-width: 260px;
}
.ml {
  margin-left: 0.5rem;
}
:deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
</style>
