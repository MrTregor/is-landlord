<script setup lang="ts">
// Календарь (14 Экраны собственника, раздел 11; FR-09): месяц, неделя, список недели; фильтры по объекту и видам.
import FullCalendar from '@fullcalendar/vue3'
import dayGridPlugin from '@fullcalendar/vue3/daygrid'
import interactionPlugin from '@fullcalendar/vue3/interaction'
import listPlugin from '@fullcalendar/vue3/list'
import ruLocale from '@fullcalendar/vue3/locales/ru'
import classicTheme from '@fullcalendar/vue3/themes/classic'
import timeGridPlugin from '@fullcalendar/vue3/timegrid'
import '@fullcalendar/vue3/skeleton.css'
import '@fullcalendar/vue3/themes/classic/theme.css'
import '@fullcalendar/vue3/themes/classic/palette.css'
import dayjs from 'dayjs'
import { computed, reactive, ref } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { useCalendarEvents, useCalendarMutations, type CalendarEvent, type CalendarQuery } from '@/api/queries/calendar'
import { useContracts } from '@/api/queries/contracts'
import { useProperties } from '@/api/queries/properties'
import type { components } from '@/api/schema'
import DateInput from '@/components/DateInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import { formatDate } from '@/utils/date'
import { calendarKindLabels, manualEventKindLabels, options } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'

type Kind = components['schemas']['CalendarEventKind']
type ManualKind = components['schemas']['ManualCalendarEventKind']
const toast = useToast()
const confirm = useConfirm()
const mut = useCalendarMutations()

// Фильтры.
const allKinds = Object.keys(calendarKindLabels) as Kind[]
const kinds = ref<Kind[]>([...allKinds])
const propertyId = ref<string | null>(null)
const properties = useProperties({ pageSize: 100 })
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((p) => ({ label: p.code, value: p.id })),
)

// Диапазон видимых дат приходит из FullCalendar (конец — не включительно).
const range = ref<{ from: string; to: string } | null>(null)
const params = computed<CalendarQuery | null>(() => {
  if (!range.value || kinds.value.length === 0) return null
  return {
    from: range.value.from,
    to: range.value.to,
    propertyId: propertyId.value ?? undefined,
    kinds: kinds.value.length === allKinds.length ? undefined : kinds.value,
  }
})
const events = useCalendarEvents(params)

function className(e: CalendarEvent): string {
  const cls = [`ev-${e.kind}`]
  if (e.done) cls.push('ev-done')
  if (e.forecast) cls.push('ev-forecast')
  return cls.join(' ')
}

const fcEvents = computed(() =>
  kinds.value.length === 0
    ? []
    : (events.data.value ?? []).map((e, i) => ({
        id: e.id ?? `computed-${i}`,
        title: e.title,
        start: e.startTime ? `${e.date}T${e.startTime}` : e.date,
        end: e.startTime && e.endTime ? `${e.date}T${e.endTime}` : undefined,
        allDay: !e.startTime,
        className: className(e),
        extendedProps: { ev: e },
      })),
)

const options_ = computed(() => ({
  plugins: [classicTheme, dayGridPlugin, timeGridPlugin, listPlugin, interactionPlugin],
  initialView: 'dayGridMonth',
  locale: ruLocale,
  firstDay: 1,
  timedText: 'По времени',
  height: 'auto',
  headerToolbar: { start: 'today prev,next', center: 'title', end: 'dayGridMonth,timeGridWeek,listWeek' },
  buttons: {
    today: { text: 'Сегодня' },
    dayGridMonth: { text: 'Месяц' },
    timeGridWeek: { text: 'Неделя' },
    listWeek: { text: 'Список недели' },
  },
  dayMaxEvents: 4,
  events: fcEvents.value,
  datesSet: (info: { startStr: string; endStr: string }) => {
    const from = info.startStr.slice(0, 10)
    const to = dayjs(info.endStr.slice(0, 10)).subtract(1, 'day').format('YYYY-MM-DD')
    if (range.value?.from !== from || range.value?.to !== to) range.value = { from, to }
  },
  eventClick: (info: { event: { extendedProps: Record<string, unknown> } }) => {
    selected.value = info.event.extendedProps.ev as CalendarEvent
    card.value = true
  },
  dateClick: (info: { dateStr: string }) => openNew(info.dateStr.slice(0, 10)),
}))

// Карточка события.
const card = ref(false)
const selected = ref<CalendarEvent | null>(null)
async function toggleDone() {
  const e = selected.value
  if (!e?.id) return
  try {
    selected.value = await mut.done.mutateAsync({ id: e.id, done: !e.done })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}
function remove() {
  const e = selected.value
  if (!e?.id) return
  confirm.require({
    header: 'Удалить событие?',
    message: `«${e.title}» на ${formatDate(e.date)}`,
    acceptProps: { label: 'Удалить', severity: 'danger' },
    rejectProps: { label: 'Отмена', outlined: true },
    accept: async () => {
      try {
        await mut.remove.mutateAsync(e.id!)
        card.value = false
        toast.add({ severity: 'success', summary: 'Событие удалено', life: 3000 })
      } catch (err) {
        toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
      }
    },
  })
}

// Диалог нового и изменяемого события.
const dialog = ref(false)
const editing = ref<CalendarEvent | null>(null)
const form = reactive({
  kind: 'showing' as ManualKind,
  title: '',
  startsOn: null as string | null,
  startTime: '',
  endTime: '',
  propertyId: null as string | null,
  contractId: null as string | null,
  notes: '',
})
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
const contracts = useContracts(() => ({
  propertyId: form.propertyId ?? undefined,
  status: ['active', 'extended'],
  pageSize: 100,
}))
const contractOptions = computed(() =>
  (contracts.data.value?.items ?? []).map((c) => ({ label: `№ ${c.number} — ${c.tenant.displayName}`, value: c.id })),
)

function openNew(day: string) {
  editing.value = null
  Object.assign(form, {
    kind: 'showing',
    title: '',
    startsOn: day,
    startTime: '',
    endTime: '',
    propertyId: propertyId.value,
    contractId: null,
    notes: '',
  })
  submitted.value = false
  serverErrors.value = {}
  serverError.value = null
  dialog.value = true
}
function openEdit() {
  const e = selected.value
  if (!e) return
  editing.value = e
  Object.assign(form, {
    kind: (e.source === 'manual' ? e.kind : 'custom') as ManualKind,
    title: e.title,
    startsOn: e.date,
    startTime: e.startTime ?? '',
    endTime: e.endTime ?? '',
    propertyId: e.property?.id ?? null,
    contractId: e.contract?.id ?? null,
    notes: e.notes ?? '',
  })
  submitted.value = false
  serverErrors.value = {}
  serverError.value = null
  card.value = false
  dialog.value = true
}
const onlyNotes = computed(() => editing.value?.source === 'auto')
const time = /^([01]\d|2[0-3]):[0-5]\d$/
const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!onlyNotes.value) {
    if (!form.title.trim()) e.title = 'Укажите название'
    if (!form.startsOn) e.startsOn = 'Укажите дату'
    if (form.startTime && !time.test(form.startTime)) e.startTime = 'Время — ЧЧ:ММ'
    if (form.endTime && !time.test(form.endTime)) e.endTime = 'Время — ЧЧ:ММ'
    if (form.endTime && (!form.startTime || form.endTime <= form.startTime))
      e.endTime = 'Время окончания — позже времени начала'
  }
  return { ...e, ...serverErrors.value }
})
const showErr = (f: string) => (submitted.value || serverErrors.value[f] ? errors.value[f] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  if (Object.keys(errors.value).length) return
  const notes = form.notes.trim() || null
  try {
    if (editing.value?.id && onlyNotes.value) {
      await mut.update.mutateAsync({ id: editing.value.id, body: { notes } })
    } else {
      const body = {
        kind: form.kind,
        title: form.title.trim(),
        startsOn: form.startsOn!,
        startTime: form.startTime || null,
        endTime: form.endTime || null,
        propertyId: form.propertyId,
        contractId: form.contractId,
        notes,
      }
      if (editing.value?.id) await mut.update.mutateAsync({ id: editing.value.id, body })
      else await mut.create.mutateAsync(body)
    }
    dialog.value = false
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
  } catch (err) {
    const p = parseProblem(err)
    serverErrors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}

const kindOptions = options(calendarKindLabels)
</script>

<template>
  <div>
    <PageHeader title="Календарь">
      <template #actions>
        <Button label="Новое событие" icon="pi pi-plus" @click="openNew(dayjs().format('YYYY-MM-DD'))" />
      </template>
    </PageHeader>

    <div class="filters">
      <Select
        v-model="propertyId"
        :options="propertyOptions"
        option-label="label"
        option-value="value"
        placeholder="Все объекты"
        aria-label="Объект"
        show-clear
        filter
      />
      <div class="kinds">
        <div v-for="k in kindOptions" :key="k.value" class="field field--check">
          <Checkbox v-model="kinds" :input-id="`k-${k.value}`" :value="k.value" />
          <label :for="`k-${k.value}`" :class="['legend', `ev-${k.value}`]">{{ k.label }}</label>
        </div>
      </div>
    </div>

    <div class="card calendar">
      <FullCalendar :options="options_" />
    </div>

    <Dialog
      v-model:visible="card"
      modal
      :header="selected?.title"
      :style="{ width: '480px' }"
      :breakpoints="{ '520px': '100vw' }"
    >
      <dl v-if="selected" class="props">
        <dt>Вид</dt>
        <dd>{{ calendarKindLabels[selected.kind] }}<template v-if="selected.forecast"> (прогноз)</template></dd>
        <dt>Дата</dt>
        <dd>
          {{ formatDate(selected.date) }}
          <template v-if="selected.startTime">
            {{ selected.startTime }}<template v-if="selected.endTime">–{{ selected.endTime }}</template></template
          >
        </dd>
        <template v-if="selected.property">
          <dt>Объект</dt>
          <dd>
            <RouterLink :to="`/properties/${selected.property.id}`">{{ selected.property.code }}</RouterLink>
          </dd>
        </template>
        <template v-if="selected.contract">
          <dt>Договор</dt>
          <dd>
            <RouterLink :to="`/contracts/${selected.contract.id}`">№ {{ selected.contract.number }}</RouterLink>
          </dd>
        </template>
        <template v-if="selected.amountKop != null">
          <dt>Сумма</dt>
          <dd><MoneyText :kop="selected.amountKop" /></dd>
        </template>
        <template v-if="selected.notes">
          <dt>Заметки</dt>
          <dd class="pre">{{ selected.notes }}</dd>
        </template>
        <dt>Состояние</dt>
        <dd>{{ selected.done ? 'выполнено' : 'не выполнено' }}</dd>
      </dl>
      <div v-if="selected?.id" class="actions">
        <Button
          :label="selected.done ? 'Снять отметку' : 'Выполнено'"
          icon="pi pi-check"
          outlined
          :loading="mut.done.isPending.value"
          @click="toggleDone"
        />
        <Button label="Изменить" icon="pi pi-pencil" outlined @click="openEdit" />
        <Button
          v-if="selected.source === 'manual'"
          label="Удалить"
          icon="pi pi-trash"
          severity="danger"
          outlined
          @click="remove"
        />
      </div>
    </Dialog>

    <Dialog
      v-model:visible="dialog"
      modal
      :header="editing ? (onlyNotes ? 'Заметки к событию' : 'Изменить событие') : 'Новое событие'"
      :style="{ width: '560px' }"
      :breakpoints="{ '600px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="save">
        <template v-if="!onlyNotes">
          <div class="field">
            <label for="e-kind">Вид *</label>
            <Select
              v-model="form.kind"
              input-id="e-kind"
              :options="options(manualEventKindLabels)"
              option-label="label"
              option-value="value"
              fluid
            />
          </div>
          <div class="field">
            <label for="e-title">Название *</label>
            <InputText id="e-title" v-model="form.title" :invalid="!!showErr('title')" fluid />
            <small v-if="showErr('title')" class="error">{{ showErr('title') }}</small>
          </div>
          <div class="form-grid">
            <div class="field">
              <label for="e-date">Дата *</label>
              <DateInput v-model="form.startsOn" input-id="e-date" :invalid="!!showErr('startsOn')" />
              <small v-if="showErr('startsOn')" class="error">{{ showErr('startsOn') }}</small>
            </div>
            <div class="field">
              <label for="e-from">Время с</label>
              <InputMask
                id="e-from"
                v-model="form.startTime"
                mask="99:99"
                placeholder="18:30"
                :invalid="!!showErr('startTime')"
              />
              <small v-if="showErr('startTime')" class="error">{{ showErr('startTime') }}</small>
            </div>
            <div class="field">
              <label for="e-to">Время по</label>
              <InputMask
                id="e-to"
                v-model="form.endTime"
                mask="99:99"
                placeholder="19:00"
                :invalid="!!showErr('endTime')"
              />
              <small v-if="showErr('endTime')" class="error">{{ showErr('endTime') }}</small>
            </div>
          </div>
          <div class="field">
            <label for="e-prop">Объект</label>
            <Select
              v-model="form.propertyId"
              input-id="e-prop"
              :options="propertyOptions"
              option-label="label"
              option-value="value"
              show-clear
              filter
              fluid
              @change="form.contractId = null"
            />
          </div>
          <div class="field">
            <label for="e-contract">Договор</label>
            <Select
              v-model="form.contractId"
              input-id="e-contract"
              :options="contractOptions"
              option-label="label"
              option-value="value"
              show-clear
              filter
              :invalid="!!showErr('contractId')"
              fluid
            />
            <small v-if="showErr('contractId')" class="error">{{ showErr('contractId') }}</small>
          </div>
        </template>
        <div class="field">
          <label for="e-notes">Заметки</label>
          <Textarea id="e-notes" v-model="form.notes" rows="3" auto-resize fluid />
        </div>
        <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="dialog = false" />
          <Button type="submit" label="Сохранить" :loading="mut.create.isPending.value || mut.update.isPending.value" />
        </div>
      </form>
    </Dialog>
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem 1.5rem;
  align-items: center;
  margin-bottom: 1rem;
}
.kinds {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem 1rem;
}
.legend {
  padding-left: 0.5rem;
  border-left: 4px solid var(--ev-color);
}
.calendar {
  padding: 1rem;
}
.props {
  display: grid;
  grid-template-columns: 110px 1fr;
  gap: 0.375rem 1rem;
  margin: 0 0 1rem;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
}
.pre {
  white-space: pre-wrap;
}
/* Цвета видов (14 разд. 11): сроки — синий, просрочки — красный, договоры и уведомления — фиолетовый,
   регистрация и индексация — оранжевый, показы — зелёный, ремонты и поверка — серо-коричневый, прочее — серый. */
.ev-payment_due {
  --ev-color: #2563eb;
}
.ev-overdue {
  --ev-color: #dc2626;
}
.ev-contract_end,
.ev-contract_end_notice {
  --ev-color: #7c3aed;
}
.ev-registration,
.ev-indexation {
  --ev-color: #ea580c;
}
.ev-showing {
  --ev-color: #16a34a;
}
.ev-repair,
.ev-meter_verification {
  --ev-color: #8b7355;
}
.ev-custom {
  --ev-color: #6b7280;
}
.calendar :deep([class*='ev-']) {
  background-color: var(--ev-color);
  border-color: var(--ev-color);
  color: #fff;
}
.calendar :deep(.ev-done) {
  opacity: 0.45;
  text-decoration: line-through;
}
.calendar :deep(.ev-forecast) {
  border-style: dashed;
  border-width: 2px;
  background-color: color-mix(in srgb, var(--ev-color) 70%, white);
}
</style>
