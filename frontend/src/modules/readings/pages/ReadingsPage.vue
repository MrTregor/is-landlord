<script setup lang="ts">
// Показания счётчиков (14 Экраны собственника, раздел 8): проверка показаний нанимателей, ввод собственником.
import dayjs from 'dayjs'
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useReadingMutations, useReadings, type Reading, type ReadingsQuery } from '@/api/queries/charges'
import { fetchMeters, useProperties, usePropertySub } from '@/api/queries/properties'
import type { components } from '@/api/schema'
import DateInput from '@/components/DateInput.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatDateTime, formatMonth, toIsoDate } from '@/utils/date'
import {
  meterTypeLabels,
  options,
  readingSourceLabels,
  readingStatusLabels,
  readingStatusSeverity,
} from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'
import { formatKop } from '@/utils/money'
import { parseProblem } from '@/utils/problem'

type Decision = components['schemas']['MeterReadingDecision']
const toast = useToast()
const m = useReadingMutations()
const { state, set } = useListQuery({ status: 'submitted' })
const str = (k: string) => (state.value.values[k] as string | undefined) || undefined
const query = computed<ReadingsQuery>(() => ({
  status: str('status') === 'all' ? undefined : (str('status') as ReadingsQuery['status']),
  propertyId: str('propertyId'),
  periodMonth: str('periodMonth'),
  page: state.value.page,
  pageSize: state.value.pageSize,
}))
const list = useReadings(query)
const properties = useProperties({ pageSize: 100 })
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((p) => ({ label: p.code, value: p.id })),
)
const num = (v: string | null | undefined) =>
  v
    ? v
        .replace(/(\.\d*?)0+$/, '$1')
        .replace(/\.$/, '')
        .replace('.', ',')
    : '—'

function announce(d: Decision) {
  if (d.utilitiesCharge) {
    toast.add({
      severity: 'success',
      summary: `Сформировано начисление ЖКУ на ${formatKop(d.utilitiesCharge.amountKop)}`,
      life: 5000,
    })
  }
}
function fail(err: unknown) {
  toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
}

async function accept(r: Reading) {
  try {
    announce(await m.accept.mutateAsync(r.id))
    toast.add({ severity: 'success', summary: 'Показание принято', life: 3000 })
  } catch (err) {
    fail(err)
  }
}
const selected = ref<Reading[]>([])
async function acceptSelected() {
  let ok = 0
  for (const r of selected.value.filter((x) => x.status === 'submitted')) {
    try {
      announce(await m.accept.mutateAsync(r.id))
      ok++
    } catch (err) {
      fail(err)
    }
  }
  selected.value = []
  if (ok) toast.add({ severity: 'success', summary: `Принято показаний: ${ok}`, life: 3000 })
}

// Отклонение с причиной.
const rejectTarget = ref<Reading | null>(null)
const rejectReason = ref('')
const rejectSubmitted = ref(false)
function openReject(r: Reading) {
  rejectTarget.value = r
  rejectReason.value = ''
  rejectSubmitted.value = false
}
async function reject() {
  rejectSubmitted.value = true
  if (rejectReason.value.trim().length < 5 || !rejectTarget.value) return
  try {
    await m.reject.mutateAsync({ id: rejectTarget.value.id, reason: rejectReason.value.trim() })
    rejectTarget.value = null
    toast.add({ severity: 'success', summary: 'Показание отклонено, нанимателю отправлено письмо', life: 3000 })
  } catch (err) {
    fail(err)
  }
}

// «Внести показание»: объект → счётчик, месяц, дата, значение.
const dialog = ref(false)
const form = reactive({
  propertyId: null as string | null,
  meterId: null as string | null,
  month: null as Date | null,
  readingDate: null as string | null,
  value: '',
})
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
const meters = usePropertySub(() => form.propertyId ?? '', 'meters', fetchMeters)
const meterOptions = computed(() =>
  (meters.data.value ?? [])
    .filter((x) => !x.archivedAt)
    .map((x) => ({ value: x.id, label: `${meterTypeLabels[x.type]}${x.serialNumber ? ` № ${x.serialNumber}` : ''}` })),
)
watch(
  () => form.propertyId,
  () => (form.meterId = null),
)
function openCreate() {
  Object.assign(form, {
    propertyId: str('propertyId') ?? null,
    meterId: null,
    month: dayjs().startOf('month').toDate(),
    readingDate: toIsoDate(new Date()),
    value: '',
  })
  submitted.value = false
  serverErrors.value = {}
  serverError.value = null
  dialog.value = true
}
const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!form.propertyId) e.propertyId = 'Выберите объект'
  if (!form.meterId) e.meterId = 'Выберите счётчик'
  if (!form.month) e.periodMonth = 'Выберите месяц'
  if (!form.readingDate) e.readingDate = 'Укажите дату'
  if (!/^\d{1,6}([.,]\d{1,3})?$/.test(form.value.trim())) e.value = 'Не больше 6 знаков до запятой и 3 после'
  return { ...e, ...serverErrors.value }
})
const err = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)
async function save() {
  submitted.value = true
  serverErrors.value = {}
  if (Object.keys(errors.value).length) return
  try {
    const d = await m.create.mutateAsync({
      meterId: form.meterId!,
      periodMonth: dayjs(form.month!).startOf('month').format('YYYY-MM-DD'),
      readingDate: form.readingDate!,
      value: form.value.trim().replace(',', '.'),
    })
    dialog.value = false
    toast.add({ severity: 'success', summary: 'Показание внесено', life: 3000 })
    announce(d)
  } catch (e) {
    const p = parseProblem(e)
    serverErrors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}

const statusOptions = [{ value: 'all', label: 'Все статусы' }, ...options(readingStatusLabels)]
const periodDate = computed(() => (str('periodMonth') ? dayjs(str('periodMonth')).toDate() : null))
</script>

<template>
  <div>
    <PageHeader title="Показания счётчиков">
      <template #actions>
        <Button
          v-if="selected.length"
          :label="`Принять выбранные (${selected.length})`"
          icon="pi pi-check-square"
          outlined
          :loading="m.accept.isPending.value"
          @click="acceptSelected"
        />
        <Button label="Внести показание" icon="pi pi-plus" @click="openCreate" />
      </template>
    </PageHeader>

    <div class="filters">
      <Select
        :model-value="str('status') ?? 'all'"
        :options="statusOptions"
        option-label="label"
        option-value="value"
        aria-label="Статус"
        @update:model-value="(v) => set({ status: v })"
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
      <DatePicker
        :model-value="periodDate"
        view="month"
        date-format="mm.yy"
        placeholder="Месяц"
        show-clear
        aria-label="Месяц"
        class="month"
        @update:model-value="
          (v) =>
            set({
              periodMonth: v
                ? dayjs(v as Date)
                    .startOf('month')
                    .format('YYYY-MM-DD')
                : undefined,
            })
        "
      />
    </div>

    <DataTable
      v-model:selection="selected"
      :value="list.data.value?.items ?? []"
      :loading="list.isFetching.value"
      lazy
      paginator
      :first="(state.page - 1) * state.pageSize"
      :rows="state.pageSize"
      :total-records="list.data.value?.total ?? 0"
      :rows-per-page-options="[25, 50, 100]"
      size="small"
      data-key="id"
      @page="(e) => set({ page: e.page + 1, pageSize: e.rows }, false)"
    >
      <template #empty>Показаний нет</template>
      <Column selection-mode="multiple" :style="{ width: '3rem' }" />
      <Column header="Месяц">
        <template #body="{ data }">{{ formatMonth((data as Reading).periodMonth) }}</template>
      </Column>
      <Column header="Объект">
        <template #body="{ data }">{{ (data as Reading).property.code }}</template>
      </Column>
      <Column header="Счётчик">
        <template #body="{ data }">
          {{ meterTypeLabels[(data as Reading).meterType] }}
          <span v-if="(data as Reading).serialNumber" class="hint">№ {{ (data as Reading).serialNumber }}</span>
        </template>
      </Column>
      <Column header="Предыдущее">
        <template #body="{ data }">
          {{ num((data as Reading).previousValue) }}
          <span v-if="(data as Reading).previousDate" class="hint"
            >({{ formatDate((data as Reading).previousDate) }})</span
          >
        </template>
      </Column>
      <Column header="Текущее">
        <template #body="{ data }">
          {{ num((data as Reading).value) }} <span class="hint">({{ formatDate((data as Reading).readingDate) }})</span>
        </template>
      </Column>
      <Column header="Расход">
        <template #body="{ data }">
          <span :class="{ high: (data as Reading).highConsumption }">{{ num((data as Reading).consumption) }}</span>
          <i
            v-if="(data as Reading).highConsumption"
            class="pi pi-exclamation-triangle high"
            title="Расход больше трёх средних за последние 3 месяца"
            aria-label="Расход больше трёх средних за последние 3 месяца"
          />
        </template>
      </Column>
      <Column header="Источник">
        <template #body="{ data }">{{ readingSourceLabels[(data as Reading).source] }}</template>
      </Column>
      <Column header="Передано">
        <template #body="{ data }">
          {{ (data as Reading).submittedBy ?? '—' }}
          <div class="hint">{{ formatDateTime((data as Reading).submittedAt) }}</div>
        </template>
      </Column>
      <Column header="Статус">
        <template #body="{ data }">
          <StatusTag
            :label="readingStatusLabels[(data as Reading).status]"
            :severity="readingStatusSeverity[(data as Reading).status]"
          />
          <div v-if="(data as Reading).rejectReason" class="hint">{{ (data as Reading).rejectReason }}</div>
        </template>
      </Column>
      <Column header="">
        <template #body="{ data }">
          <div v-if="(data as Reading).status === 'submitted'" class="row-actions">
            <Button label="Принять" size="small" text @click="accept(data as Reading)" />
            <Button label="Отклонить" size="small" text severity="danger" @click="openReject(data as Reading)" />
          </div>
        </template>
      </Column>
    </DataTable>

    <Dialog
      :visible="!!rejectTarget"
      modal
      header="Отклонить показание"
      :style="{ width: '440px' }"
      :breakpoints="{ '480px': '100vw' }"
      @update:visible="(v: boolean) => !v && (rejectTarget = null)"
    >
      <form class="form" novalidate @submit.prevent="reject">
        <div class="field">
          <label for="rj-reason">Причина *</label>
          <Textarea
            id="rj-reason"
            v-model="rejectReason"
            rows="2"
            auto-resize
            :invalid="rejectSubmitted && rejectReason.trim().length < 5"
            fluid
          />
          <small v-if="rejectSubmitted && rejectReason.trim().length < 5" class="error"
            >Причина — не короче 5 символов</small
          >
        </div>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="rejectTarget = null" />
          <Button type="submit" label="Отклонить" severity="danger" :loading="m.reject.isPending.value" />
        </div>
      </form>
    </Dialog>

    <Dialog
      v-model:visible="dialog"
      modal
      header="Внести показание"
      :style="{ width: '480px' }"
      :breakpoints="{ '520px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="save">
        <div class="field">
          <label for="rd-prop">Объект *</label>
          <Select
            v-model="form.propertyId"
            input-id="rd-prop"
            :options="propertyOptions"
            option-label="label"
            option-value="value"
            filter
            :invalid="!!err('propertyId')"
            fluid
          />
          <small v-if="err('propertyId')" class="error">{{ err('propertyId') }}</small>
        </div>
        <div class="field">
          <label for="rd-meter">Счётчик *</label>
          <Select
            v-model="form.meterId"
            input-id="rd-meter"
            :options="meterOptions"
            option-label="label"
            option-value="value"
            :disabled="!form.propertyId"
            :invalid="!!err('meterId')"
            fluid
          />
          <small v-if="err('meterId')" class="error">{{ err('meterId') }}</small>
        </div>
        <div class="form-grid">
          <div class="field">
            <label for="rd-month">Месяц *</label>
            <DatePicker
              v-model="form.month"
              input-id="rd-month"
              view="month"
              date-format="mm.yy"
              show-icon
              icon-display="input"
              :invalid="!!err('periodMonth')"
              fluid
            />
            <small v-if="err('periodMonth')" class="error">{{ err('periodMonth') }}</small>
          </div>
          <div class="field">
            <label for="rd-date">Дата *</label>
            <DateInput
              v-model="form.readingDate"
              input-id="rd-date"
              :max-date="new Date()"
              :invalid="!!err('readingDate')"
            />
            <small v-if="err('readingDate')" class="error">{{ err('readingDate') }}</small>
          </div>
        </div>
        <div class="field">
          <label for="rd-value">Значение *</label>
          <InputText id="rd-value" v-model="form.value" inputmode="decimal" :invalid="!!err('value')" fluid />
          <small v-if="err('value')" class="error">{{ err('value') }}</small>
        </div>
        <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="dialog = false" />
          <Button type="submit" label="Сохранить" :loading="m.create.isPending.value" />
        </div>
      </form>
    </Dialog>
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
.month {
  width: 9rem;
}
.high {
  color: var(--p-orange-600);
  font-weight: 600;
  margin-left: 0.25rem;
}
.row-actions {
  display: flex;
}
</style>
