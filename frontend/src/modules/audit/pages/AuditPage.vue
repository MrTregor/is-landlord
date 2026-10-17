<script setup lang="ts">
// Журнал действий (14 Экраны собственника, раздел 14; FR-12.2). Фильтры и страница — в адресной строке.
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import { xlsxUrl } from '@/api/queries/reports'
import { useToast } from 'primevue/usetoast'
import { api, unwrap } from '@/api/client'
import type { components } from '@/api/schema'
import AuditDiff from '@/components/AuditDiff.vue'
import PageHeader from '@/components/PageHeader.vue'
import { formatDateTime, fromIsoDate, toIsoDate } from '@/utils/date'
import { auditActorRoleLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { actionOptions, entityTypeOptions } from '../actions'

type Entry = components['schemas']['AuditEntryDTO']

const route = useRoute()
const router = useRouter()
const toast = useToast()

const str = (v: unknown) => (typeof v === 'string' && v !== '' ? v : undefined)
const query = computed(() => ({
  from: str(route.query.from),
  to: str(route.query.to),
  actorUserId: str(route.query.actorUserId),
  action: str(route.query.action),
  entityType: str(route.query.entityType),
  entityId: str(route.query.entityId),
  q: str(route.query.q),
  page: Number(route.query.page ?? 1) || 1,
  pageSize: Number(route.query.pageSize ?? 25) || 25,
}))
/** Выгрузка журнала XLSX с фильтрами списка (до 50 000 строк, 11 разд. 7). */
const exportUrl = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { page, pageSize, ...rest } = query.value
  return xlsxUrl('/audit.xlsx', rest)
})

function setQuery(patch: Record<string, string | number | undefined>, resetPage = true) {
  const next: Record<string, string> = {}
  for (const [k, v] of Object.entries({ ...query.value, ...(resetPage ? { page: 1 } : {}), ...patch })) {
    if (v !== undefined && v !== '' && !(k === 'page' && v === 1) && !(k === 'pageSize' && v === 25))
      next[k] = String(v)
  }
  void router.replace({ query: next })
}

const audit = useQuery({
  queryKey: ['audit', 'list', query],
  queryFn: () => unwrap(api.GET('/audit', { params: { query: query.value } })),
  placeholderData: keepPreviousData,
})
const owners = useQuery({
  queryKey: ['users', 'list', { role: 'owner', page: 1, pageSize: 100 }],
  queryFn: () => unwrap(api.GET('/users', { params: { query: { role: 'owner', page: 1, pageSize: 100 } } })),
})
const userOptions = computed(() => (owners.data.value?.items ?? []).map((u) => ({ value: u.id, label: u.displayName })))

// Период — диапазон дат.
const period = ref<Date[] | null>(null)
watch(
  () => [query.value.from, query.value.to],
  ([from, to]) => {
    period.value = from ? [fromIsoDate(from), ...(to ? [fromIsoDate(to)] : [])] : null
  },
  { immediate: true },
)
function onPeriod(v: Date[] | null) {
  const [from, to] = v ?? []
  if (v && !to) return
  setQuery({ from: from ? toIsoDate(from) : undefined, to: to ? toIsoDate(to) : undefined })
}

const search = ref(query.value.q ?? '')
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => setQuery({ q: v.trim() || undefined }), 300)
})

function reset() {
  search.value = ''
  void router.replace({ query: {} })
}

// Подпись сущности: вид и короткий идентификатор (имена подставятся с модулями объектов и договоров).
const entityTitles: Record<string, string> = Object.fromEntries(entityTypeOptions.map((o) => [o.value, o.label]))
function entityLabel(e: Entry): string {
  const id = e.entityId.length === 36 ? e.entityId.slice(0, 8) : e.entityId
  return `${entityTitles[e.entityType] ?? e.entityType} · ${id}`
}

const expanded = ref<Record<string, boolean>>({})

// Проверка целостности цепочки хешей.
const verifying = ref(false)
async function verify() {
  verifying.value = true
  try {
    const r = await unwrap(api.GET('/audit/verify'))
    if (r.ok) {
      toast.add({
        severity: 'success',
        summary: `Цепочка записей не нарушена, проверено ${r.checked} записей`,
        life: 6000,
      })
    } else {
      toast.add({ severity: 'error', summary: `Нарушение целостности в записи № ${r.brokenAtId}`, life: 0 })
    }
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  } finally {
    verifying.value = false
  }
}
</script>

<template>
  <div>
    <PageHeader title="Журнал действий">
      <template #actions>
        <Button as="a" :href="exportUrl" download label="Выгрузить в Excel" icon="pi pi-file-excel" outlined />
        <Button label="Проверить целостность" icon="pi pi-shield" :loading="verifying" @click="verify" />
      </template>
    </PageHeader>

    <div class="filters">
      <DatePicker
        :model-value="period"
        selection-mode="range"
        date-format="dd.mm.yy"
        show-icon
        placeholder="Период"
        aria-label="Период"
        :manual-input="false"
        @update:model-value="(v) => onPeriod(v as Date[] | null)"
      />
      <Select
        :model-value="query.actorUserId"
        :options="userOptions"
        option-label="label"
        option-value="value"
        placeholder="Пользователь"
        aria-label="Пользователь"
        show-clear
        @update:model-value="(v) => setQuery({ actorUserId: v ?? undefined })"
      />
      <Select
        :model-value="query.action"
        :options="actionOptions"
        option-label="label"
        option-value="value"
        placeholder="Действие"
        aria-label="Действие"
        filter
        show-clear
        @update:model-value="(v) => setQuery({ action: v ?? undefined })"
      />
      <Select
        :model-value="query.entityType"
        :options="entityTypeOptions"
        option-label="label"
        option-value="value"
        placeholder="Сущность"
        aria-label="Вид сущности"
        show-clear
        @update:model-value="(v) => setQuery({ entityType: v ?? undefined })"
      />
      <IconField>
        <InputIcon class="pi pi-search" />
        <InputText v-model="search" placeholder="Поиск по описанию" aria-label="Поиск по описанию" />
      </IconField>
      <Button label="Сбросить" text @click="reset" />
    </div>

    <DataTable
      v-model:expanded-rows="expanded"
      :value="audit.data.value?.items ?? []"
      :loading="audit.isFetching.value"
      lazy
      paginator
      :first="(query.page - 1) * query.pageSize"
      :rows="query.pageSize"
      :total-records="audit.data.value?.total ?? 0"
      :rows-per-page-options="[25, 50, 100]"
      striped-rows
      size="small"
      data-key="id"
      @page="(e) => setQuery({ page: e.page + 1, pageSize: e.rows }, false)"
    >
      <template #empty>Записей нет</template>
      <Column expander :style="{ width: '3rem' }" />
      <Column header="Дата и время">
        <template #body="{ data }">{{ formatDateTime((data as Entry).occurredAt) }}</template>
      </Column>
      <Column header="Пользователь">
        <template #body="{ data }">
          {{ (data as Entry).actorName ?? auditActorRoleLabels[(data as Entry).actorRole] }}
        </template>
      </Column>
      <Column field="actionTitle" header="Действие" />
      <Column header="Объект действия">
        <template #body="{ data }">
          {{ (data as Entry).entityTitle ?? entityLabel(data as Entry) }}
        </template>
      </Column>
      <Column field="summary" header="Описание" />
      <template #expansion="{ data }">
        <AuditDiff :before="(data as Entry).before" :after="(data as Entry).after" />
        <small v-if="(data as Entry).ip" class="hint">IP-адрес: {{ (data as Entry).ip }}</small>
      </template>
    </DataTable>
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin-bottom: 1rem;
}
</style>
