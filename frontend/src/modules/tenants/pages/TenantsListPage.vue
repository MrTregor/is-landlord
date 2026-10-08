<script setup lang="ts">
// Наниматели (14 Экраны собственника, раздел 3.1).
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useTenants, type TenantsQuery } from '@/api/queries/tenants'
import type { components } from '@/api/schema'
import EmptyState from '@/components/EmptyState.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import { cabinetStatusLabels, options, tenantKindLabels } from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'
import { formatPhone } from '@/utils/phone'

type Item = components['schemas']['TenantListItem']
const router = useRouter()
const { state, set, reset } = useListQuery()

const params = computed<TenantsQuery>(() => {
  const v = state.value.values
  return {
    q: (v.q as string | undefined) || undefined,
    kind: v.kind as TenantsQuery['kind'],
    hasActiveContract: v.active === '1' ? true : undefined,
    anonymized: v.anonymized === '1' ? true : undefined,
    page: state.value.page,
    pageSize: state.value.pageSize,
  }
})
const list = useTenants(params)

const search = ref((state.value.values.q as string | undefined) ?? '')
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => set({ q: v.trim() || undefined }), 300)
})
const filtersEmpty = computed(() => !Object.values(state.value.values).some((v) => v))
</script>

<template>
  <div>
    <PageHeader title="Наниматели">
      <template #actions>
        <Button label="Добавить нанимателя" icon="pi pi-user-plus" @click="router.push('/tenants/new')" />
      </template>
    </PageHeader>

    <div class="filters">
      <IconField>
        <InputIcon class="pi pi-search" />
        <InputText
          v-model="search"
          placeholder="ФИО, наименование, ИНН, телефон, e-mail"
          aria-label="Поиск"
          class="filters__search"
        />
      </IconField>
      <Select
        :model-value="state.values.kind"
        :options="options(tenantKindLabels)"
        option-label="label"
        option-value="value"
        placeholder="Тип"
        aria-label="Тип"
        show-clear
        @update:model-value="(v) => set({ kind: v ?? undefined })"
      />
      <div class="field--check field">
        <Checkbox
          input-id="t-active"
          binary
          :model-value="state.values.active === '1'"
          @update:model-value="(v: boolean) => set({ active: v ? '1' : undefined })"
        />
        <label for="t-active">С действующим договором</label>
      </div>
      <div class="field--check field">
        <Checkbox
          input-id="t-anon"
          binary
          :model-value="state.values.anonymized === '1'"
          @update:model-value="(v: boolean) => set({ anonymized: v ? '1' : undefined })"
        />
        <label for="t-anon">Показать обезличенных</label>
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
      striped-rows
      size="small"
      data-key="id"
      row-hover
      @page="(e) => set({ page: e.page + 1, pageSize: e.rows }, false)"
      @row-click="(e) => router.push(`/tenants/${(e.data as Item).id}`)"
    >
      <template #empty>
        <EmptyState
          v-if="filtersEmpty && !list.isFetching.value"
          text="Нанимателей пока нет. Добавьте первого нанимателя"
          action-label="Добавить нанимателя"
          icon="pi pi-users"
          @action="router.push('/tenants/new')"
        />
        <span v-else>Ничего не найдено</span>
      </template>
      <Column header="Наниматель">
        <template #body="{ data }">
          <RouterLink :to="`/tenants/${(data as Item).id}`" @click.stop>{{ (data as Item).displayName }}</RouterLink>
          <Tag v-if="(data as Item).anonymized" value="обезличен" severity="secondary" class="ml" />
        </template>
      </Column>
      <Column header="Тип">
        <template #body="{ data }">{{ tenantKindLabels[(data as Item).kind] }}</template>
      </Column>
      <Column header="Телефон">
        <template #body="{ data }">{{ formatPhone((data as Item).phone) || '—' }}</template>
      </Column>
      <Column header="E-mail">
        <template #body="{ data }">{{ (data as Item).email ?? '—' }}</template>
      </Column>
      <Column field="activeContracts" header="Договоров действующих" />
      <Column header="Долг">
        <template #body="{ data }"><MoneyText :kop="(data as Item).debtKop" /></template>
      </Column>
      <Column header="Кабинет">
        <template #body="{ data }">{{ cabinetStatusLabels[(data as Item).cabinetStatus] }}</template>
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
.filters__search {
  min-width: 300px;
}
.ml {
  margin-left: 0.5rem;
}
:deep(.p-datatable-tbody > tr) {
  cursor: pointer;
}
</style>
