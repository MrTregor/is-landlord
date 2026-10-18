<script setup lang="ts">
// Настройки → Служебное (14 Экраны собственника, раздел 16, вкладка 9): плановые задачи, история запусков, резервные
// копии; в режиме demo — «Дата системы» и «Выполнить ежедневные задачи» (07 «Порядок выполнения на защите»).
import { computed, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import {
  useBackups,
  useDevClock,
  useDevMutations,
  useJobRuns,
  useJobs,
  useRunJob,
  type JobName,
} from '@/api/queries/jobs'
import { useAppInfo } from '@/api/queries/public'
import DateInput from '@/components/DateInput.vue'
import type { components } from '@/api/schema'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import { jobRunStatusLabels, jobRunStatusSeverity, jobTriggerLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { cronText, formatSize, statsText } from '../jobs'

type Job = components['schemas']['JobDTO']
type Run = components['schemas']['JobRunDTO']
type Backup = components['schemas']['BackupFile']
const toast = useToast()
const jobs = useJobs()
const run = useRunJob()
const filter = ref<JobName | undefined>()
const page = ref(1)
const runs = useJobRuns(filter, page)
const title = (name: string) => jobs.data.value?.find((j) => j.name === name)?.title ?? name

const appInfo = useAppInfo()
const isDemo = computed(() => appInfo.data.value?.env === 'demo')
const showDocs = computed(() => !!appInfo.data.value && appInfo.data.value.env !== 'prod')
const backups = useBackups()

// Демо: дата системы и ежедневные задачи.
const clock = useDevClock(isDemo)
const dev = useDevMutations()
const demoDate = ref<string | null>(null)
watch(
  () => clock.data.value?.today,
  (v) => {
    if (v) demoDate.value = v
  },
  { immediate: true },
)
const dailyResult = ref<Run[] | null>(null)

async function applyDate() {
  if (!demoDate.value) return
  try {
    const c = await dev.set.mutateAsync(demoDate.value)
    toast.add({ severity: 'success', summary: `Дата системы: ${formatDate(c.today)}`, life: 3000 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}

async function resetDate() {
  try {
    const c = await dev.reset.mutateAsync()
    toast.add({ severity: 'success', summary: `Дата системы сброшена: ${formatDate(c.today)}`, life: 3000 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}

async function runDaily() {
  try {
    const r = await dev.runDaily.mutateAsync()
    dailyResult.value = r.jobs
    const failed = r.jobs.filter((j) => j.status === 'failed').length
    toast.add({
      severity: failed ? 'warn' : 'success',
      summary: `Ежедневные задачи за ${formatDate(r.today)} выполнены${failed ? `, с ошибками: ${failed}` : ''}`,
      life: 5000,
    })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}

async function start(j: Job) {
  try {
    await run.mutateAsync(j.name)
    toast.add({ severity: 'success', summary: `Задача «${j.title}» поставлена`, life: 3000 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}
</script>

<template>
  <div>
    <template v-if="isDemo">
      <h3 class="subtitle">Демонстрационный режим</h3>
      <div class="demo">
        <div class="field">
          <label for="demo-date">Дата системы</label>
          <div class="inline">
            <DateInput v-model="demoDate" input-id="demo-date" />
            <Button label="Применить" :loading="dev.set.isPending.value" :disabled="!demoDate" @click="applyDate" />
            <Button label="Сбросить" outlined :loading="dev.reset.isPending.value" @click="resetDate" />
          </div>
          <small class="hint">Сдвиг времени общий для сервера и фоновых задач.</small>
        </div>
        <Button
          label="Выполнить ежедневные задачи"
          icon="pi pi-play"
          :loading="dev.runDaily.isPending.value"
          @click="runDaily"
        />
      </div>
      <DataTable v-if="dailyResult" :value="dailyResult" size="small" data-key="id" class="daily">
        <Column header="Задача">
          <template #body="{ data }">{{ title((data as Run).jobName) }}</template>
        </Column>
        <Column header="Результат">
          <template #body="{ data }">
            <StatusTag
              :label="jobRunStatusLabels[(data as Run).status]"
              :severity="jobRunStatusSeverity[(data as Run).status]"
            />
          </template>
        </Column>
        <Column header="Итоги">
          <template #body="{ data }">
            {{ statsText((data as Run).stats) }}
            <div v-if="(data as Run).errorText" class="error">{{ (data as Run).errorText }}</div>
          </template>
        </Column>
      </DataTable>
    </template>

    <h3 class="subtitle">Плановые задачи</h3>
    <DataTable :value="jobs.data.value ?? []" :loading="jobs.isPending.value" size="small" data-key="name">
      <Column header="Задача">
        <template #body="{ data }">
          {{ (data as Job).title }}
          <div class="hint">{{ (data as Job).name }}</div>
        </template>
      </Column>
      <Column header="Расписание">
        <template #body="{ data }">{{ cronText((data as Job).schedule) }}</template>
      </Column>
      <Column header="Последний запуск">
        <template #body="{ data }">
          <template v-if="(data as Job).lastRun">
            {{ formatDateTime((data as Job).lastRun!.startedAt) }}
            <div class="hint">{{ jobTriggerLabels[(data as Job).lastRun!.trigger] }}</div>
          </template>
          <span v-else class="hint">не запускалась</span>
        </template>
      </Column>
      <Column header="Результат">
        <template #body="{ data }">
          <template v-if="(data as Job).lastRun">
            <StatusTag
              :label="jobRunStatusLabels[(data as Job).lastRun!.status]"
              :severity="jobRunStatusSeverity[(data as Job).lastRun!.status]"
            />
            <div class="hint">{{ statsText((data as Job).lastRun!.stats) }}</div>
          </template>
        </template>
      </Column>
      <Column header="">
        <template #body="{ data }">
          <Button
            label="Запустить сейчас"
            size="small"
            outlined
            :loading="run.isPending.value"
            @click="start(data as Job)"
          />
        </template>
      </Column>
    </DataTable>

    <h3 class="subtitle">История запусков</h3>
    <Select
      v-model="filter"
      :options="(jobs.data.value ?? []).map((j) => ({ label: j.title, value: j.name }))"
      option-label="label"
      option-value="value"
      placeholder="Все задачи"
      aria-label="Задача"
      show-clear
      class="filter"
      @change="page = 1"
    />
    <DataTable
      :value="runs.data.value?.items ?? []"
      :loading="runs.isFetching.value"
      lazy
      paginator
      :rows="25"
      :first="(page - 1) * 25"
      :total-records="runs.data.value?.total ?? 0"
      size="small"
      data-key="id"
      @page="(e) => (page = e.page + 1)"
    >
      <template #empty>Запусков нет</template>
      <Column header="Задача">
        <template #body="{ data }">{{ title((data as Run).jobName) }}</template>
      </Column>
      <Column header="Дата">
        <template #body="{ data }">{{ formatDate((data as Run).businessDate) }}</template>
      </Column>
      <Column header="Запуск">
        <template #body="{ data }">
          {{ formatDateTime((data as Run).startedAt) }} · {{ jobTriggerLabels[(data as Run).trigger] }}
        </template>
      </Column>
      <Column header="Результат">
        <template #body="{ data }">
          <StatusTag
            :label="jobRunStatusLabels[(data as Run).status]"
            :severity="jobRunStatusSeverity[(data as Run).status]"
          />
        </template>
      </Column>
      <Column header="Итоги">
        <template #body="{ data }">
          {{ statsText((data as Run).stats) }}
          <div v-if="(data as Run).errorText" class="error">{{ (data as Run).errorText }}</div>
        </template>
      </Column>
    </DataTable>

    <h3 class="subtitle">Резервные копии</h3>
    <DataTable :value="backups.data.value ?? []" :loading="backups.isPending.value" size="small" data-key="fileName">
      <template #empty>Резервных копий нет</template>
      <Column header="Файл" field="fileName" />
      <Column header="Дата">
        <template #body="{ data }">{{ formatDateTime((data as Backup).createdAt) }}</template>
      </Column>
      <Column header="Размер">
        <template #body="{ data }">{{ formatSize((data as Backup).sizeBytes) }}</template>
      </Column>
    </DataTable>
    <p class="hint">Восстановление — по инструкции docs/deploy.md.</p>

    <template v-if="showDocs">
      <h3 class="subtitle">Описание API</h3>
      <a href="/api/v1/docs" target="_blank" rel="noopener">Открыть описание API (OpenAPI)</a>
    </template>
  </div>
</template>

<style scoped>
.subtitle {
  font-size: 1rem;
  margin: 1.5rem 0 0.75rem;
}
.subtitle:first-child {
  margin-top: 0;
}
.filter {
  margin-bottom: 0.75rem;
  min-width: 260px;
}
.demo {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 1rem 1.5rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.inline {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.daily {
  margin-top: 1rem;
}
.error {
  color: var(--p-red-600);
}
</style>
