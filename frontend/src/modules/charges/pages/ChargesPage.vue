<script setup lang="ts">
// Начисления (14 Экраны собственника, раздел 5): фильтры в адресной строке, итоги, боковая панель начисления.
import dayjs from 'dayjs'
import { computed, ref, watch } from 'vue'
import type { ChargesQuery } from '@/api/queries/charges'
import { useProperties } from '@/api/queries/properties'
import DateInput from '@/components/DateInput.vue'
import { xlsxUrl } from '@/api/queries/reports'
import PageHeader from '@/components/PageHeader.vue'
import { chargeKindLabel, chargeStatusLabels, options } from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'
import ChargeDrawer from '../components/ChargeDrawer.vue'
import ChargesTable from '../components/ChargesTable.vue'
import ManualChargeDialog from '../components/ManualChargeDialog.vue'

const { state, set, reset } = useListQuery({}, ['kind', 'status'])
const kinds = (['rent', 'utilities', 'penalty', 'damage', 'other'] as const).map((k) => ({
  value: k,
  label: chargeKindLabel(k),
}))
const properties = useProperties({ pageSize: 100 })
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((p) => ({ label: p.code, value: p.id })),
)

const str = (k: string) => (state.value.values[k] as string | undefined) || undefined
const query = computed<ChargesQuery>(() => ({
  q: str('q'),
  kind: (state.value.values.kind as ChargesQuery['kind'])?.length
    ? (state.value.values.kind as ChargesQuery['kind'])
    : undefined,
  status: (state.value.values.status as ChargesQuery['status'])?.length
    ? (state.value.values.status as ChargesQuery['status'])
    : undefined,
  overdue: state.value.values.overdue === '1' ? true : undefined,
  propertyId: str('propertyId'),
  periodFrom: str('periodFrom'),
  periodTo: str('periodTo'),
  dueFrom: str('dueFrom'),
  dueTo: str('dueTo'),
  sort: state.value.sort as ChargesQuery['sort'],
  page: state.value.page,
  pageSize: state.value.pageSize,
}))
/** Выгрузка XLSX с теми же фильтрами, без страницы (11 разд. 8). */
const exportUrl = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { page, pageSize, ...rest } = query.value as Record<string, unknown>
  return xlsxUrl('/charges.xlsx', rest as Parameters<typeof xlsxUrl>[1])
})

const search = ref(str('q') ?? '')
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => set({ q: v.trim() || undefined }), 300)
})

// Период — выбор месяца; в адрес пишется первое число.
const monthDate = (k: string) => (str(k) ? dayjs(str(k)).toDate() : null)
const setMonth = (k: string, v: Date | null | undefined) =>
  set({ [k]: v ? dayjs(v).startOf('month').format('YYYY-MM-DD') : undefined })

const opened = ref<string | null>(null)
const manualDialog = ref(false)
</script>

<template>
  <div>
    <PageHeader title="Начисления">
      <template #actions>
        <Button as="a" :href="exportUrl" download label="Выгрузить в Excel" icon="pi pi-file-excel" outlined />
        <Button label="Ручное начисление" icon="pi pi-plus" @click="manualDialog = true" />
      </template>
    </PageHeader>

    <div class="filters">
      <IconField>
        <InputIcon class="pi pi-search" />
        <InputText v-model="search" placeholder="Название, договор, наниматель" aria-label="Поиск" />
      </IconField>
      <MultiSelect
        :model-value="(state.values.kind as string[] | undefined) ?? []"
        :options="kinds"
        option-label="label"
        option-value="value"
        placeholder="Вид"
        aria-label="Вид"
        display="chip"
        @update:model-value="(v: string[]) => set({ kind: v })"
      />
      <MultiSelect
        :model-value="(state.values.status as string[] | undefined) ?? []"
        :options="options(chargeStatusLabels)"
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
        show-clear
        filter
        @update:model-value="(v) => set({ propertyId: v ?? undefined })"
      />
      <div class="field field--check">
        <Checkbox
          input-id="c-overdue"
          binary
          :model-value="state.values.overdue === '1'"
          @update:model-value="(v: boolean) => set({ overdue: v ? '1' : undefined })"
        />
        <label for="c-overdue">Только просроченные</label>
      </div>
    </div>
    <div class="filters">
      <span class="hint">Период:</span>
      <DatePicker
        :model-value="monthDate('periodFrom')"
        view="month"
        date-format="mm.yy"
        placeholder="с месяца"
        show-clear
        aria-label="Период с"
        class="month"
        @update:model-value="(v) => setMonth('periodFrom', v as Date | null)"
      />
      <DatePicker
        :model-value="monthDate('periodTo')"
        view="month"
        date-format="mm.yy"
        placeholder="по месяц"
        show-clear
        aria-label="Период по"
        class="month"
        @update:model-value="(v) => setMonth('periodTo', v as Date | null)"
      />
      <span class="hint">Срок оплаты:</span>
      <div class="date">
        <DateInput
          :model-value="str('dueFrom') ?? null"
          placeholder="с"
          @update:model-value="(v) => set({ dueFrom: v ?? undefined })"
        />
      </div>
      <div class="date">
        <DateInput
          :model-value="str('dueTo') ?? null"
          placeholder="по"
          @update:model-value="(v) => set({ dueTo: v ?? undefined })"
        />
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

    <ChargesTable
      :query="query"
      show-contract
      @page="(p, s) => set({ page: p, pageSize: s }, false)"
      @sort="(s) => set({ sort: s }, false)"
      @open="(id) => (opened = id)"
    />
    <ChargeDrawer v-model:charge-id="opened" />
    <ManualChargeDialog v-model:visible="manualDialog" />
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
.month {
  width: 9rem;
}
.date {
  width: 11rem;
}
</style>
