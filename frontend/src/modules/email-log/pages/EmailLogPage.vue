<script setup lang="ts">
// Письма (14 Экраны собственника, раздел 14; FR-10): фильтры «Статус», «Шаблон», «Период», поиск по адресу и теме;
// таблица журнала отправки; просмотр письма — диалог с HTML; «Отправить повторно».
import { computed, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import {
  useEmailLog,
  useEmailTemplates,
  useResendEmail,
  type EmailLogItem,
  type EmailLogQuery,
} from '@/api/queries/notify'
import DateInput from '@/components/DateInput.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDateTime } from '@/utils/date'
import { emailStatusLabels, emailStatusSeverity, options } from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'
import { parseProblem } from '@/utils/problem'

const toast = useToast()
const { state, set, reset } = useListQuery()
const str = (k: string) => (state.value.values[k] as string | undefined) || undefined
const query = computed<EmailLogQuery>(() => ({
  status: str('status') as EmailLogQuery['status'],
  templateCode: str('templateCode') as EmailLogQuery['templateCode'],
  from: str('from'),
  to: str('to'),
  q: str('q'),
  page: state.value.page,
  pageSize: state.value.pageSize,
}))
const list = useEmailLog(query)
const templates = useEmailTemplates()
const templateOptions = computed(() => (templates.data.value ?? []).map((t) => ({ value: t.code, label: t.name })))
const templateName = (code: string) => templates.data.value?.find((t) => t.code === code)?.name ?? code
const resend = useResendEmail()
const e = (x: unknown) => x as EmailLogItem

const search = ref(str('q') ?? '')
function applySearch() {
  set({ q: search.value.trim() || undefined })
}
function resetAll() {
  search.value = ''
  reset()
}

const viewing = ref<EmailLogItem | null>(null)
const viewVisible = computed({
  get: () => viewing.value !== null,
  set: (v: boolean) => {
    if (!v) viewing.value = null
  },
})

async function doResend(m: EmailLogItem) {
  try {
    await resend.mutateAsync(m.id)
    toast.add({ severity: 'success', summary: 'Письмо поставлено в очередь повторно', life: 3000 })
    viewing.value = null
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}
</script>

<template>
  <div>
    <PageHeader title="Письма" />

    <div class="filters">
      <form class="search" role="search" @submit.prevent="applySearch">
        <IconField>
          <InputIcon class="pi pi-search" />
          <InputText v-model="search" placeholder="Адрес или тема" aria-label="Поиск по адресу и теме" />
        </IconField>
      </form>
      <Select
        :model-value="state.values.status"
        :options="options(emailStatusLabels)"
        option-label="label"
        option-value="value"
        placeholder="Статус"
        aria-label="Статус"
        show-clear
        @update:model-value="(v) => set({ status: v ?? undefined })"
      />
      <Select
        :model-value="state.values.templateCode"
        :options="templateOptions"
        option-label="label"
        option-value="value"
        placeholder="Шаблон"
        aria-label="Шаблон"
        filter
        show-clear
        @update:model-value="(v) => set({ templateCode: v ?? undefined })"
      />
      <span class="hint">Период:</span>
      <div class="date">
        <DateInput
          :model-value="str('from') ?? null"
          placeholder="с"
          @update:model-value="(v) => set({ from: v ?? undefined })"
        />
      </div>
      <div class="date">
        <DateInput
          :model-value="str('to') ?? null"
          placeholder="по"
          @update:model-value="(v) => set({ to: v ?? undefined })"
        />
      </div>
      <Button label="Сбросить" text @click="resetAll" />
    </div>

    <Message v-if="list.isError.value" severity="error" :closable="false">
      {{ parseProblem(list.error.value).message }}
    </Message>
    <DataTable
      :value="list.data.value?.items ?? []"
      :loading="list.isFetching.value && !list.data.value"
      lazy
      paginator
      :first="((query.page ?? 1) - 1) * (query.pageSize ?? 25)"
      :rows="query.pageSize ?? 25"
      :total-records="list.data.value?.total ?? 0"
      :rows-per-page-options="[25, 50, 100]"
      size="small"
      striped-rows
      row-hover
      data-key="id"
      class="clickable"
      @page="(ev) => set({ page: ev.page + 1, pageSize: ev.rows }, false)"
      @row-click="(ev) => (viewing = e(ev.data))"
    >
      <template #empty>Писем нет</template>
      <Column header="Создано">
        <template #body="{ data }">{{ formatDateTime(e(data).createdAt) }}</template>
      </Column>
      <Column header="Отправлено">
        <template #body="{ data }">{{ e(data).sentAt ? formatDateTime(e(data).sentAt!) : '—' }}</template>
      </Column>
      <Column header="Кому">
        <template #body="{ data }">{{ e(data).toEmail }}</template>
      </Column>
      <Column header="Тема">
        <template #body="{ data }">{{ e(data).subject }}</template>
      </Column>
      <Column header="Шаблон">
        <template #body="{ data }">{{ templateName(e(data).templateCode) }}</template>
      </Column>
      <Column header="Статус">
        <template #body="{ data }">
          <StatusTag :label="emailStatusLabels[e(data).status]" :severity="emailStatusSeverity[e(data).status]" />
        </template>
      </Column>
      <Column header="Попыток" field="attempts" />
      <Column header="Ошибка">
        <template #body="{ data }">
          <span class="error">{{ e(data).lastError ?? '' }}</span>
        </template>
      </Column>
    </DataTable>

    <Dialog
      v-model:visible="viewVisible"
      modal
      :header="viewing?.subject ?? 'Письмо'"
      :style="{ width: '720px' }"
      :breakpoints="{ '760px': '100vw' }"
    >
      <template v-if="viewing">
        <dl class="props">
          <dt>Кому</dt>
          <dd>{{ viewing.toEmail }}</dd>
          <dt>Статус</dt>
          <dd>
            <StatusTag :label="emailStatusLabels[viewing.status]" :severity="emailStatusSeverity[viewing.status]" />
            <span v-if="viewing.lastError" class="error">{{ viewing.lastError }}</span>
          </dd>
          <template v-if="viewing.attachments.length">
            <dt>Вложения</dt>
            <dd>
              <a
                v-for="a in viewing.attachments"
                :key="a.fileId"
                :href="`/api/v1/files/${a.fileId}`"
                target="_blank"
                rel="noopener"
                >{{ a.name }}</a
              >
            </dd>
          </template>
        </dl>
        <iframe class="frame" title="Текст письма" sandbox="" :srcdoc="viewing.bodyHtml" />
      </template>
      <template #footer>
        <Button label="Закрыть" outlined @click="viewing = null" />
        <Button
          label="Отправить повторно"
          icon="pi pi-send"
          :disabled="!viewing || !viewing.toEmail.includes('@')"
          :loading="resend.isPending.value"
          @click="viewing && doResend(viewing)"
        />
      </template>
    </Dialog>
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
.date {
  width: 11rem;
}
.error {
  color: var(--p-red-600);
}
.hint {
  color: var(--p-text-muted-color);
}
.clickable :deep(tbody tr) {
  cursor: pointer;
}
.props {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.25rem 1rem;
  margin: 0 0 0.75rem;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.frame {
  width: 100%;
  height: 55vh;
  border: 1px solid var(--p-content-border-color);
  border-radius: 6px;
  background: #fff;
}
</style>
