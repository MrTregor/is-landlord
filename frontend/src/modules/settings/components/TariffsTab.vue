<script setup lang="ts">
// Настройки → Тарифы (14 Экраны собственника, раздел 16, вкладка 6). Цена — строка с 4 знаками, без float.
import { computed, reactive, ref } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { useProperties, useTariffMutations, useTariffs, type Tariff } from '@/api/queries/properties'
import DateInput from '@/components/DateInput.vue'
import { formatDate } from '@/utils/date'
import { meterTypeLabels, options } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { todayIso } from '@/utils/today'

const tariffs = useTariffs()
const m = useTariffMutations()
const properties = useProperties({ pageSize: 100 })
const toast = useToast()
const confirm = useConfirm()

const propertyOptions = computed(() => [
  { label: 'Все объекты', value: null as string | null },
  ...(properties.data.value?.items ?? []).map((p) => ({
    label: `${p.code} — ${p.address}`,
    value: p.id as string | null,
  })),
])

/** «12.3400» → «12,34». */
function priceText(v: string): string {
  const [int, frac = ''] = v.split('.')
  return `${int},${frac
    .padEnd(2, '0')
    .slice(0, 4)
    .replace(/(\d{2})0+$/, '$1')}`
}

const dialog = ref(false)
const form = reactive({
  meterType: 'cold_water' as Tariff['meterType'],
  propertyId: null as string | null,
  price: '',
  effectiveFrom: todayIso() as string | null,
  note: '',
})
const errors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)

function open() {
  Object.assign(form, {
    meterType: 'cold_water',
    propertyId: null,
    price: '',
    effectiveFrom: todayIso(),
    note: '',
  })
  errors.value = {}
  serverError.value = null
  dialog.value = true
}

async function save() {
  const e: Record<string, string> = {}
  const price = form.price.trim().replace(',', '.')
  if (!/^\d{1,8}(\.\d{1,4})?$/.test(price) || /^0+(\.0+)?$/.test(price))
    e.price = 'Цена больше нуля, не более 4 знаков после запятой'
  if (!form.effectiveFrom) e.effectiveFrom = 'Укажите дату'
  errors.value = e
  if (Object.keys(e).length) return
  serverError.value = null
  try {
    await m.create.mutateAsync({
      meterType: form.meterType,
      propertyId: form.propertyId,
      price,
      effectiveFrom: form.effectiveFrom!,
      note: form.note.trim() || null,
    })
    dialog.value = false
    toast.add({ severity: 'success', summary: 'Тариф добавлен', life: 3000 })
  } catch (err) {
    const p = parseProblem(err)
    errors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}

function remove(t: Tariff) {
  confirm.require({
    header: 'Удалить тариф?',
    message: `${meterTypeLabels[t.meterType]} с ${formatDate(t.effectiveFrom)}`,
    acceptProps: { label: 'Удалить', severity: 'danger' },
    rejectProps: { label: 'Отмена', outlined: true },
    accept: async () => {
      try {
        await m.remove.mutateAsync(t.id)
        toast.add({ severity: 'success', summary: 'Тариф удалён', life: 3000 })
      } catch (err) {
        toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
      }
    },
  })
}
</script>

<template>
  <div>
    <div class="actions actions--start">
      <Button label="Добавить тариф" icon="pi pi-plus" @click="open" />
    </div>
    <Message v-if="tariffs.isError.value" severity="error" :closable="false">{{
      parseProblem(tariffs.error.value).message
    }}</Message>
    <DataTable
      :value="tariffs.data.value ?? []"
      :loading="tariffs.isFetching.value"
      size="small"
      data-key="id"
      striped-rows
    >
      <template #empty>Тарифов нет</template>
      <Column header="Ресурс">
        <template #body="{ data }">{{ meterTypeLabels[(data as Tariff).meterType] }}</template>
      </Column>
      <Column header="Объект">
        <template #body="{ data }">{{ (data as Tariff).propertyCode ?? 'Все объекты' }}</template>
      </Column>
      <Column header="Цена за единицу">
        <template #body="{ data }"
          ><span class="tabular-nums">{{ priceText((data as Tariff).price) }} ₽</span></template
        >
      </Column>
      <Column header="С даты">
        <template #body="{ data }">{{ formatDate((data as Tariff).effectiveFrom) }}</template>
      </Column>
      <Column header="Примечание">
        <template #body="{ data }">{{ (data as Tariff).note ?? '' }}</template>
      </Column>
      <Column header="" style="width: 3rem">
        <template #body="{ data }">
          <Button icon="pi pi-trash" text severity="danger" aria-label="Удалить" @click="remove(data as Tariff)" />
        </template>
      </Column>
    </DataTable>

    <Dialog
      v-model:visible="dialog"
      modal
      header="Новый тариф"
      :style="{ width: '520px' }"
      :breakpoints="{ '575px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="save">
        <div class="field">
          <label for="t-type">Ресурс *</label>
          <Select
            v-model="form.meterType"
            input-id="t-type"
            :options="options(meterTypeLabels)"
            option-label="label"
            option-value="value"
            fluid
          />
        </div>
        <div class="field">
          <label for="t-prop">Объект</label>
          <Select
            v-model="form.propertyId"
            input-id="t-prop"
            :options="propertyOptions"
            option-label="label"
            option-value="value"
            filter
            fluid
          />
          <small class="hint">Тариф по объекту важнее общего.</small>
        </div>
        <div class="field">
          <label for="t-price">Цена за единицу, ₽ *</label>
          <InputText
            id="t-price"
            v-model="form.price"
            inputmode="decimal"
            placeholder="0,0000"
            :invalid="!!errors.price"
            fluid
          />
          <small v-if="errors.price" class="error">{{ errors.price }}</small>
        </div>
        <div class="field">
          <label for="t-from">С даты *</label>
          <DateInput v-model="form.effectiveFrom" input-id="t-from" :invalid="!!errors.effectiveFrom" />
          <small v-if="errors.effectiveFrom" class="error">{{ errors.effectiveFrom }}</small>
        </div>
        <div class="field">
          <label for="t-note">Примечание</label>
          <InputText id="t-note" v-model="form.note" fluid />
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
.actions--start {
  justify-content: flex-start;
  margin-bottom: 1rem;
}
</style>
