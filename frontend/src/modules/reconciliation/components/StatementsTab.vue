<script setup lang="ts">
// Вкладка «Выписки банка» экрана «Сверка» (14 Экраны собственника, раздел 7): загрузка выписки 1С или CSV,
// итог обработки и таблица загрузок.
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import {
  csvTemplateUrl,
  useBankStatements,
  useStatementMutations,
  type BankStatement,
} from '@/api/queries/reconciliation'
import { formatDate, formatDateTime } from '@/utils/date'
import { statementFormatLabels } from '@/utils/labels'
import { ApiError, parseProblem } from '@/utils/problem'

const router = useRouter()
const toast = useToast()
const m = useStatementMutations()

const page = ref(1)
const pageSize = ref(25)
const list = useBankStatements(() => ({ page: page.value, pageSize: pageSize.value }))

const input = ref<HTMLInputElement | null>(null)
const dragging = ref(false)
const errors = ref<string[]>([])
const MAX_BYTES = 2 * 1024 * 1024

async function upload(file: File | undefined) {
  if (!file) return
  errors.value = []
  if (file.size > MAX_BYTES) {
    errors.value = ['Файл больше 2 МБ']
    return
  }
  try {
    const st = await m.upload.mutateAsync(file)
    toast.add({
      severity: 'success',
      summary:
        `Обработано строк: ${st.linesIncoming}, сопоставлено: ${st.linesMatched}, ` +
        `не опознано: ${st.linesUnmatched}, дублей: ${st.linesDuplicate}`,
      life: 6000,
    })
    void router.push(`/reconciliation/statements/${st.id}`)
  } catch (e) {
    // Ошибки разбора приходят списком «Строка N: …» — показываются все.
    const all = e instanceof ApiError ? (e.problem.errors ?? []).map((x) => x.message) : []
    errors.value = all.length ? all : [parseProblem(e).message]
  } finally {
    if (input.value) input.value.value = ''
  }
}

function onDrop(e: DragEvent) {
  dragging.value = false
  void upload(e.dataTransfer?.files?.[0])
}
function onPick(e: Event) {
  void upload((e.target as HTMLInputElement).files?.[0])
}

const period = (s: BankStatement) =>
  s.dateFrom || s.dateTo ? `${formatDate(s.dateFrom) || '…'} – ${formatDate(s.dateTo) || '…'}` : '—'
const items = computed(() => list.data.value?.items ?? [])
const st = (x: unknown) => x as BankStatement
</script>

<template>
  <div>
    <div
      class="drop"
      :class="{ 'drop--active': dragging }"
      role="button"
      tabindex="0"
      aria-label="Загрузить файл выписки"
      @click="input?.click()"
      @keydown.enter.prevent="input?.click()"
      @keydown.space.prevent="input?.click()"
      @dragover.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @drop.prevent="onDrop"
    >
      <i class="pi pi-upload drop__icon" aria-hidden="true" />
      <span v-if="m.upload.isPending.value">Выписка обрабатывается…</span>
      <span v-else>Перетащите файл выписки (формат 1С или CSV) или нажмите, чтобы выбрать</span>
      <input ref="input" type="file" accept=".txt,.csv,text/plain,text/csv" hidden @change="onPick" />
    </div>
    <p class="hint"><a :href="csvTemplateUrl" download>Скачать образец CSV</a> · размер файла — до 2 МБ</p>
    <Message v-if="errors.length" severity="error" :closable="false" class="errors">
      <div>Файл не загружен:</div>
      <ul>
        <li v-for="(e, i) in errors" :key="i">{{ e }}</li>
      </ul>
    </Message>

    <DataTable
      :value="items"
      :loading="list.isFetching.value"
      lazy
      paginator
      :first="(page - 1) * pageSize"
      :rows="pageSize"
      :total-records="list.data.value?.total ?? 0"
      :rows-per-page-options="[25, 50, 100]"
      size="small"
      striped-rows
      data-key="id"
      row-hover
      class="clickable"
      @page="
        (e) => {
          page = e.page + 1
          pageSize = e.rows
        }
      "
      @row-click="(e) => router.push(`/reconciliation/statements/${st(e.data).id}`)"
    >
      <template #empty>Выписки ещё не загружались</template>
      <Column header="Дата загрузки">
        <template #body="{ data }">{{ formatDateTime(st(data).uploadedAt) }}</template>
      </Column>
      <Column header="Файл">
        <template #body="{ data }">
          <RouterLink :to="`/reconciliation/statements/${st(data).id}`" @click.stop>{{ st(data).fileName }}</RouterLink>
          <div class="hint">{{ statementFormatLabels[st(data).format] }}</div>
        </template>
      </Column>
      <Column header="Период">
        <template #body="{ data }">{{ period(st(data)) }}</template>
      </Column>
      <Column header="Счёт">
        <template #body="{ data }">{{ st(data).accountNumber ?? '—' }}</template>
      </Column>
      <Column header="Строк" field="linesTotal" />
      <Column header="Сопоставлено" field="linesMatched" />
      <Column header="Не опознано" field="linesUnmatched" />
      <Column header="Дубли" field="linesDuplicate" />
    </DataTable>
  </div>
</template>

<style scoped>
.drop {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.5rem;
  padding: 1.5rem 1rem;
  border: 2px dashed var(--p-content-border-color);
  border-radius: var(--p-content-border-radius);
  text-align: center;
  cursor: pointer;
}
.drop:focus-visible,
.drop--active {
  border-color: var(--p-primary-color);
  outline: none;
}
.drop__icon {
  font-size: 1.5rem;
  color: var(--p-primary-color);
}
.errors ul {
  margin: 0.25rem 0 0;
  padding-left: 1.25rem;
}
.clickable :deep(tbody tr) {
  cursor: pointer;
}
</style>
