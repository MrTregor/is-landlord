<script setup lang="ts">
// Настройки → Служебное (14 Экраны собственника, раздел 16, вкладка 9): плановые задачи и история запусков.
import { ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useJobRuns, useJobs, useRunJob, type JobName } from '@/api/queries/jobs'
import type { components } from '@/api/schema'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import { jobRunStatusLabels, jobRunStatusSeverity, jobTriggerLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { cronText, statsText } from '../jobs'

type Job = components['schemas']['JobDTO']
type Run = components['schemas']['JobRunDTO']
const toast = useToast()
const jobs = useJobs()
const run = useRunJob()
const filter = ref<JobName | undefined>()
const page = ref(1)
const runs = useJobRuns(filter, page)
const title = (name: string) => jobs.data.value?.find((j) => j.name === name)?.title ?? name

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
    <Message severity="info" :closable="false">
      Список резервных копий появится на этапе M14. Восстановление — по инструкции docs/deploy.md.
    </Message>
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
</style>
