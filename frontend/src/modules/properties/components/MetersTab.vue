<script setup lang="ts">
// Счётчики объекта (14 Экраны собственника, раздел 2.3, вкладка 6).
import { reactive, ref } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { fetchMeters, usePropertyMutations, usePropertySub, type Meter } from '@/api/queries/properties'
import DateInput from '@/components/DateInput.vue'
import { formatDate } from '@/utils/date'
import { meterTypeLabels, options } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { todayIso } from '@/utils/today'

const props = defineProps<{ propertyId: string; archived: boolean }>()
const meters = usePropertySub(() => props.propertyId, 'meters', fetchMeters)
const m = usePropertyMutations(() => props.propertyId)
const toast = useToast()
const confirm = useConfirm()

const reading = (v: string) => v.replace('.', ',')

// Диалог «Добавить / изменить счётчик».
const dialog = ref(false)
const editing = ref<Meter | null>(null)
const form = reactive({
  type: 'cold_water' as Meter['type'],
  serialNumber: '',
  initialValue: '',
  initialDate: todayIso() as string | null,
  verificationDue: null as string | null,
  note: '',
})
const errors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)

function open(meter: Meter | null) {
  editing.value = meter
  Object.assign(form, {
    type: meter?.type ?? 'cold_water',
    serialNumber: meter?.serialNumber ?? '',
    initialValue: meter ? reading(meter.initialValue) : '',
    initialDate: meter?.initialDate ?? todayIso(),
    verificationDue: meter?.verificationDue ?? null,
    note: meter?.note ?? '',
  })
  errors.value = {}
  serverError.value = null
  dialog.value = true
}

function validate(): boolean {
  const e: Record<string, string> = {}
  const v = form.initialValue.trim().replace(',', '.')
  if (!/^\d{1,6}(\.\d{1,3})?$/.test(v)) e.initialValue = 'Не более 6 знаков до запятой и 3 после'
  if (!form.initialDate) e.initialDate = 'Укажите дату'
  errors.value = e
  return Object.keys(e).length === 0
}

async function save() {
  if (!validate()) return
  serverError.value = null
  const body = {
    type: form.type,
    serialNumber: form.serialNumber.trim() || null,
    initialValue: form.initialValue.trim().replace(',', '.'),
    initialDate: form.initialDate!,
    verificationDue: form.verificationDue,
    note: form.note.trim() || null,
  }
  try {
    if (editing.value) await m.updateMeter.mutateAsync({ meterId: editing.value.id, body })
    else await m.createMeter.mutateAsync(body)
    dialog.value = false
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
  } catch (err) {
    const p = parseProblem(err)
    errors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}

function archive(meter: Meter) {
  confirm.require({
    header: 'Перевести счётчик в архив?',
    message: `${meterTypeLabels[meter.type]}: показания по нему больше не будут приниматься.`,
    acceptLabel: 'В архив',
    rejectLabel: 'Отмена',
    accept: async () => {
      try {
        await m.archiveMeter.mutateAsync(meter.id)
      } catch (err) {
        toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
      }
    },
  })
}
</script>

<template>
  <div class="form">
    <div class="actions">
      <Button label="Добавить счётчик" icon="pi pi-plus" :disabled="archived" @click="open(null)" />
    </div>
    <DataTable
      :value="meters.data.value ?? []"
      :loading="meters.isFetching.value"
      size="small"
      striped-rows
      data-key="id"
    >
      <template #empty>Счётчиков нет</template>
      <Column header="Тип">
        <template #body="{ data }">{{ meterTypeLabels[(data as Meter).type] }}</template>
      </Column>
      <Column header="Заводской номер">
        <template #body="{ data }">{{ (data as Meter).serialNumber ?? '—' }}</template>
      </Column>
      <Column header="Начальное показание">
        <template #body="{ data }">
          {{ reading((data as Meter).initialValue) }} ({{ formatDate((data as Meter).initialDate) }})
        </template>
      </Column>
      <Column header="Последнее показание">
        <template #body="{ data }">
          <template v-if="(data as Meter).lastReading">
            {{ reading((data as Meter).lastReading!.value) }} ({{
              formatDate((data as Meter).lastReading!.readingDate)
            }})
          </template>
          <span v-else class="hint">—</span>
        </template>
      </Column>
      <Column header="Поверка до">
        <template #body="{ data }">{{ formatDate((data as Meter).verificationDue) || '—' }}</template>
      </Column>
      <Column header="Статус">
        <template #body="{ data }">
          <Tag v-if="(data as Meter).archivedAt" value="В архиве" severity="secondary" />
          <Tag v-else value="Действует" severity="success" />
        </template>
      </Column>
      <Column :style="{ width: '1%', whiteSpace: 'nowrap' }">
        <template #body="{ data }">
          <template v-if="!(data as Meter).archivedAt">
            <Button text size="small" label="Изменить" @click="open(data as Meter)" />
            <Button text size="small" label="В архив" @click="archive(data as Meter)" />
          </template>
        </template>
      </Column>
    </DataTable>

    <Dialog
      v-model:visible="dialog"
      modal
      :header="editing ? 'Изменение счётчика' : 'Новый счётчик'"
      :style="{ width: '480px' }"
      :breakpoints="{ '575px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="save">
        <div class="field">
          <label for="m-type">Тип *</label>
          <Select
            v-model="form.type"
            input-id="m-type"
            :options="options(meterTypeLabels)"
            option-label="label"
            option-value="value"
            :invalid="!!errors.type"
            fluid
          />
          <small v-if="errors.type" class="error">{{ errors.type }}</small>
        </div>
        <div class="field">
          <label for="m-serial">Заводской номер</label>
          <InputText id="m-serial" v-model="form.serialNumber" fluid />
        </div>
        <div class="field">
          <label for="m-initial">Начальное показание *</label>
          <InputText
            id="m-initial"
            v-model="form.initialValue"
            inputmode="decimal"
            :invalid="!!errors.initialValue"
            fluid
          />
          <small v-if="errors.initialValue" class="error">{{ errors.initialValue }}</small>
        </div>
        <div class="field">
          <label for="m-date">Дата начального показания *</label>
          <DateInput v-model="form.initialDate" input-id="m-date" :invalid="!!errors.initialDate" />
          <small v-if="errors.initialDate" class="error">{{ errors.initialDate }}</small>
        </div>
        <div class="field">
          <label for="m-verify">Дата поверки</label>
          <DateInput v-model="form.verificationDue" input-id="m-verify" />
          <small class="hint">Напоминание появится в календаре</small>
        </div>
        <div class="field">
          <label for="m-note">Примечание</label>
          <InputText id="m-note" v-model="form.note" fluid />
        </div>
        <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="dialog = false" />
          <Button
            type="submit"
            label="Сохранить"
            :loading="m.createMeter.isPending.value || m.updateMeter.isPending.value"
          />
        </div>
      </form>
    </Dialog>
  </div>
</template>
