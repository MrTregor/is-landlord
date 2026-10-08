<script setup lang="ts">
// Объекты (14 Экраны собственника, раздел 2.1).
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useProperties, type PropertiesQuery, type PropertyListItem } from '@/api/queries/properties'
import EmptyState from '@/components/EmptyState.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import {
  fiscalModeLabels,
  options,
  propertyKindLabels,
  propertyStatusLabels,
  propertyStatusSeverity,
} from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'

const router = useRouter()
const { state, set, reset } = useListQuery({}, ['status'])

const params = computed<PropertiesQuery>(() => {
  const v = state.value.values
  return {
    q: (v.q as string | undefined) || undefined,
    status: (v.status as PropertiesQuery['status']) || undefined,
    kind: v.kind as PropertiesQuery['kind'],
    archived: v.archived === '1' ? true : undefined,
    sort: state.value.sort as PropertiesQuery['sort'],
    page: state.value.page,
    pageSize: state.value.pageSize,
  }
})
const list = useProperties(params)

const search = ref((state.value.values.q as string | undefined) ?? '')
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => set({ q: v.trim() || undefined }), 300)
})

const sortField = computed(() => state.value.sort?.replace(/^-/, ''))
const sortOrder = computed(() => (state.value.sort?.startsWith('-') ? -1 : 1))
function onSort(e: { sortField?: unknown; sortOrder?: number | null }) {
  const f = typeof e.sortField === 'string' ? e.sortField : undefined
  set({ sort: f ? (e.sortOrder === -1 ? `-${f}` : f) : undefined }, false)
}

const filtersEmpty = computed(() => !Object.values(state.value.values).some((v) => (Array.isArray(v) ? v.length : v)))
</script>

<template>
  <div>
    <PageHeader title="Объекты">
      <template #actions>
        <Button label="Добавить объект" icon="pi pi-plus" @click="router.push('/properties/new')" />
      </template>
    </PageHeader>

    <div class="filters">
      <IconField>
        <InputIcon class="pi pi-search" />
        <InputText v-model="search" placeholder="Код или адрес" aria-label="Поиск по коду и адресу" />
      </IconField>
      <MultiSelect
        :model-value="(state.values.status as string[] | undefined) ?? []"
        :options="options(propertyStatusLabels)"
        option-label="label"
        option-value="value"
        placeholder="Статус"
        aria-label="Статус"
        display="chip"
        @update:model-value="(v: string[]) => set({ status: v })"
      />
      <Select
        :model-value="state.values.kind"
        :options="options(propertyKindLabels)"
        option-label="label"
        option-value="value"
        placeholder="Вид"
        aria-label="Вид"
        show-clear
        @update:model-value="(v) => set({ kind: v ?? undefined })"
      />
      <div class="field--check field">
        <Checkbox
          input-id="prop-archived"
          binary
          :model-value="state.values.archived === '1'"
          @update:model-value="(v: boolean) => set({ archived: v ? '1' : undefined })"
        />
        <label for="prop-archived">Показать архивные</label>
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
      sort-mode="single"
      removable-sort
      striped-rows
      size="small"
      data-key="id"
      row-hover
      @page="(e) => set({ page: e.page + 1, pageSize: e.rows }, false)"
      @sort="onSort"
      @row-click="(e) => router.push(`/properties/${(e.data as PropertyListItem).id}`)"
    >
      <template #empty>
        <EmptyState
          v-if="filtersEmpty && !list.isFetching.value"
          text="Объектов пока нет. Добавьте первый объект"
          action-label="Добавить объект"
          icon="pi pi-building"
          @action="router.push('/properties/new')"
        />
        <span v-else>Ничего не найдено</span>
      </template>
      <Column field="code" header="Объект" sortable>
        <template #body="{ data }">
          <RouterLink :to="`/properties/${(data as PropertyListItem).id}`" @click.stop>
            {{ (data as PropertyListItem).code }}
          </RouterLink>
        </template>
      </Column>
      <Column header="Вид">
        <template #body="{ data }">{{ propertyKindLabels[(data as PropertyListItem).kind] }}</template>
      </Column>
      <Column field="address" header="Адрес" />
      <Column header="Площадь, м²">
        <template #body="{ data }">{{ (data as PropertyListItem).areaSqm.replace('.', ',') }}</template>
      </Column>
      <Column field="status" header="Статус" sortable>
        <template #body="{ data }">
          <StatusTag
            :label="
              (data as PropertyListItem).archived ? 'В архиве' : propertyStatusLabels[(data as PropertyListItem).status]
            "
            :severity="
              (data as PropertyListItem).archived
                ? 'secondary'
                : propertyStatusSeverity[(data as PropertyListItem).status]
            "
          />
        </template>
      </Column>
      <Column header="Текущий договор">
        <template #body="{ data }">
          <template v-if="(data as PropertyListItem).currentContract">
            <RouterLink :to="`/contracts/${(data as PropertyListItem).currentContract!.id}`" @click.stop>
              № {{ (data as PropertyListItem).currentContract!.number }}
            </RouterLink>
            · {{ (data as PropertyListItem).currentContract!.tenantName }}, до
            {{ formatDate((data as PropertyListItem).currentContract!.endDate) }}
          </template>
          <span v-else class="hint">—</span>
        </template>
      </Column>
      <Column header="Долг">
        <template #body="{ data }"><MoneyText :kop="(data as PropertyListItem).debtKop" /></template>
      </Column>
      <Column header="Режим чека">
        <template #body="{ data }">{{ fiscalModeLabels[(data as PropertyListItem).fiscalMode] }}</template>
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
:deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
</style>
