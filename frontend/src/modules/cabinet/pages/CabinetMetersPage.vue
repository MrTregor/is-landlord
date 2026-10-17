<script setup lang="ts">
// Показания счётчиков (15 Экраны нанимателя, раздел 5): окно приёма, ввод по всем счётчикам объекта с расходом
// на лету, статусы после передачи, история за 12 месяцев.
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useCabinetMeters, useSubmitReadings, type CabinetMeter } from '@/api/queries/cabinet'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatMonth } from '@/utils/date'
import { meterTypeLabels, meterTypeShort, readingStatusLabels, readingStatusSeverity } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { useCabinetContractId } from '../useContract'
import { consumption, formatMilli, formatReading, isHigh, parseReading, recentConsumptions, toMilli } from '../readings'

const toast = useToast()
const { summary, contractId, current, contractOptions, selectContract } = useCabinetContractId()
const meters = useCabinetMeters(contractId)
const submit = useSubmitReadings()
const data = computed(() => meters.data.value)
const win = computed(() => data.value?.window)

/** «20.10» — дата окна без года. */
const dayMonth = (iso: string) => formatDate(iso).slice(0, 5)
const unit = (m: CabinetMeter) => meterTypeLabels[m.type].split(', ')[1] ?? ''

// Поля — для счётчиков без показания за месяц или с отклонённым показанием, пока окно открыто.
const editable = computed(() =>
  win.value?.open ? (data.value?.meters ?? []).filter((m) => !m.current || m.current.status === 'rejected') : [],
)
const isEditable = (m: CabinetMeter) => editable.value.some((e) => e.meterId === m.meterId)
const values = reactive<Record<string, string>>({})
const serverErrors = ref<Record<string, string>>({})
const formError = ref<string | null>(null)
watch(contractId, () => {
  for (const k of Object.keys(values)) delete values[k]
  serverErrors.value = {}
})

function parsed(m: CabinetMeter): string | null {
  return parseReading(values[m.meterId] ?? '')
}

function fieldError(m: CabinetMeter): string | null {
  if (serverErrors.value[m.meterId]) return serverErrors.value[m.meterId]!
  const raw = values[m.meterId] ?? ''
  if (!raw.trim()) return null
  const v = parsed(m)
  if (!v) return 'Введите число: до 6 знаков до запятой и 3 после'
  if (m.lastValue && toMilli(v) < toMilli(m.lastValue)) {
    return `Показание меньше предыдущего (${formatReading(m.lastValue)} от ${formatDate(m.lastDate)})`
  }
  return null
}

function usage(m: CabinetMeter): number | null {
  const v = parsed(m)
  return v ? consumption(m.lastValue, v) : null
}

function high(m: CabinetMeter): boolean {
  const c = usage(m)
  return c !== null && c >= 0 && isHigh(c, recentConsumptions(m.history))
}

const canSubmit = computed(
  () => editable.value.length > 0 && editable.value.every((m) => parsed(m) !== null && fieldError(m) === null),
)

function onInput(m: CabinetMeter) {
  if (serverErrors.value[m.meterId]) {
    const next = { ...serverErrors.value }
    delete next[m.meterId]
    serverErrors.value = next
  }
}

async function send() {
  if (!canSubmit.value || !win.value || !contractId.value) return
  formError.value = null
  serverErrors.value = {}
  const list = editable.value
  try {
    await submit.mutateAsync({
      contractId: contractId.value,
      periodMonth: win.value.periodMonth,
      readings: list.map((m) => ({ meterId: m.meterId, value: parsed(m)! })),
    })
    for (const m of list) delete values[m.meterId]
    toast.add({ severity: 'success', summary: 'Показания переданы', life: 3000 })
  } catch (e) {
    const p = parseProblem(e)
    const errs: Record<string, string> = {}
    for (const [field, msg] of Object.entries(p.fieldErrors)) {
      const i = /^readings\[(\d+)]/.exec(field)?.[1]
      const m = i !== undefined ? list[Number(i)] : undefined
      if (m) errs[m.meterId] = msg
    }
    serverErrors.value = errs
    if (!Object.keys(errs).length) formError.value = p.message
  }
}

function statusText(m: CabinetMeter): string {
  const c = m.current
  if (!c) return ''
  return c.status === 'rejected' && c.rejectReason
    ? `${readingStatusLabels.rejected}: ${c.rejectReason}`
    : readingStatusLabels[c.status]
}
</script>

<template>
  <div class="meters">
    <Select
      v-if="contractOptions.length > 1"
      :model-value="contractId"
      :options="contractOptions"
      option-label="label"
      option-value="value"
      aria-label="Договор"
      fluid
      @update:model-value="selectContract"
    />
    <Message v-if="summary.isError.value || meters.isError.value" severity="error" :closable="false">
      {{ parseProblem(summary.error.value ?? meters.error.value).message }}
    </Message>
    <Message v-else-if="summary.data.value && !contractId" severity="info" :closable="false"
      >Договоров пока нет.</Message
    >
    <Skeleton v-else-if="!data" height="10rem" />
    <template v-else-if="win">
      <header>
        <h1 class="title">Показания за {{ formatMonth(win.periodMonth) }}</h1>
        <p class="hint">Приём показаний с {{ dayMonth(win.from) }} по {{ dayMonth(win.to) }}</p>
      </header>
      <Message v-if="!data.meters.length" severity="info" :closable="false">
        По договору{{ current ? ` № ${current.number}` : '' }} показания счётчиков не передаются.
      </Message>
      <template v-else>
        <Message v-if="!win.open" severity="info" :closable="false">
          Приём показаний за {{ formatMonth(win.periodMonth) }} начнётся {{ formatDate(win.from) }}.
        </Message>
        <section v-for="m in data.meters" :key="m.meterId" class="card">
          <div class="card__head">
            <strong>{{ meterTypeShort[m.type] }}</strong>
            <span v-if="m.serialNumber" class="hint">№ {{ m.serialNumber }}</span>
          </div>
          <div v-if="m.lastValue" class="hint">
            Предыдущее: {{ formatReading(m.lastValue) }} ({{ formatDate(m.lastDate) }})
          </div>
          <div v-if="m.current" class="status">
            <StatusTag :label="statusText(m)" :severity="readingStatusSeverity[m.current.status]" />
            <span>{{ formatReading(m.current.value) }} {{ unit(m) }}</span>
          </div>
          <div v-if="isEditable(m) || !win.open" class="field">
            <label :for="`rd-${m.meterId}`">
              {{ m.current?.status === 'rejected' ? 'Новое показание' : 'Показание' }}, {{ unit(m) }}
            </label>
            <InputText
              :id="`rd-${m.meterId}`"
              v-model="values[m.meterId]"
              inputmode="decimal"
              autocomplete="off"
              placeholder="0,000"
              :disabled="!win.open || !isEditable(m)"
              :invalid="!!fieldError(m)"
              fluid
              @input="onInput(m)"
            />
            <small v-if="fieldError(m)" class="error">{{ fieldError(m) }}</small>
            <small v-else-if="usage(m) !== null" class="hint">Расход: {{ formatMilli(usage(m)!) }} {{ unit(m) }}</small>
            <small v-if="!fieldError(m) && high(m)" class="warn">
              Расход заметно выше обычного. Проверьте значение
            </small>
          </div>
          <details v-if="m.history.length" class="history">
            <summary>История показаний</summary>
            <ul>
              <li v-for="(h, i) in m.history" :key="i">
                <span>{{ formatMonth(h.periodMonth) }}</span>
                <span>{{ formatReading(h.value) }}</span>
                <StatusTag :label="readingStatusLabels[h.status]" :severity="readingStatusSeverity[h.status]" />
              </li>
            </ul>
          </details>
        </section>
        <Message v-if="formError" severity="error" :closable="false">{{ formError }}</Message>
        <Button
          v-if="editable.length"
          label="Передать показания"
          icon="pi pi-send"
          class="wide"
          :disabled="!canSubmit"
          :loading="submit.isPending.value"
          @click="send"
        />
      </template>
    </template>
  </div>
</template>

<style scoped>
.meters {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.title {
  font-size: 1.25rem;
  margin: 0;
}
header p {
  margin: 0.25rem 0 0;
}
.card {
  background: var(--p-content-background);
  border: 1px solid var(--p-content-border-color);
  border-radius: 8px;
  padding: 0.75rem 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-width: 0;
}
.card__head {
  display: flex;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.status {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.field :deep(input) {
  min-height: 44px;
}
.error {
  color: var(--p-red-600);
}
.warn {
  color: var(--p-orange-600);
}
.history summary {
  cursor: pointer;
  min-height: 44px;
  display: flex;
  align-items: center;
  color: var(--p-primary-color);
}
.history ul {
  list-style: none;
  margin: 0;
  padding: 0;
}
.history li {
  display: grid;
  grid-template-columns: 1fr auto auto;
  align-items: center;
  gap: 0.75rem;
  padding: 0.25rem 0;
  border-bottom: 1px solid var(--p-content-border-color);
}
.wide {
  width: 100%;
  min-height: 44px;
}
</style>
