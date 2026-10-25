<script setup lang="ts">
// Вкладка «Акты» (14 Экраны собственника, раздел 4.3, вкладка 7; 05 разд. 4.10): приёма-передачи и возврата.
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { fetchActs, useContractMutations, useContractSub, type Act, type ContractDetail } from '@/api/queries/contracts'
import { documentPdfUrl, useDocumentMutations } from '@/api/queries/documents'
import DateInput from '@/components/DateInput.vue'
import { formatDate } from '@/utils/date'
import { actKindLabels, meterTypeLabels } from '@/utils/labels'
import { openInNewTab } from '@/utils/pdf'
import { parseProblem } from '@/utils/problem'
import { today, todayIso } from '@/utils/today'

const props = defineProps<{ contract: ContractDetail; returnSignal: number }>()
const toast = useToast()
const acts = useContractSub(() => props.contract.id, 'acts', fetchActs)
const m = useContractMutations(() => props.contract.id)
const docs = useDocumentMutations()

/** PDF акта: у подписанного — сформированный при подписании, у черновика — формируется заново. */
async function actPdf(a: Act) {
  try {
    await openInNewTab(async () =>
      a.signed && a.documentId ? documentPdfUrl(a.documentId) : documentPdfUrl((await docs.act.mutateAsync(a.id)).id),
    )
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}

const handover = computed(() => acts.data.value?.find((a) => a.kind === 'handover') ?? null)
const ret = computed(() => acts.data.value?.find((a) => a.kind === 'return') ?? null)
const canCreateReturn = computed(() => props.contract.status !== 'draft' && !ret.value)

const reading = (v: string | null | undefined) => (v ? v.replace('.', ',') : '—')

async function createReturn() {
  try {
    const a = await m.createReturnAct.mutateAsync()
    toast.add({ severity: 'success', summary: 'Акт возврата создан', life: 3000 })
    openEditor(a)
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}
watch(
  () => props.returnSignal,
  (n) => {
    if (!n) return
    if (ret.value) openEditor(ret.value)
    else if (canCreateReturn.value) void createReturn()
  },
)

// Редактор акта.
interface Row {
  name: string
  quantity: number | null
  condition: string
  note: string
}
const editor = ref(false)
const current = ref<Act | null>(null)
const form = reactive({
  actDate: null as string | null,
  keysCount: 0 as number | null,
  conditionNotes: '',
  damages: '',
  inventory: [] as Row[],
  readings: {} as Record<string, string>,
})
const submitted = ref(false)
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)

function openEditor(a: Act) {
  current.value = a
  Object.assign(form, {
    actDate: a.actDate,
    keysCount: a.keysCount,
    conditionNotes: a.conditionNotes ?? '',
    damages: a.damages ?? '',
    inventory: a.inventory.map((i) => ({
      name: i.name,
      quantity: i.quantity,
      condition: i.condition,
      note: i.note ?? '',
    })),
    readings: Object.fromEntries(a.readings.map((r) => [r.meterId, reading(r.value) === '—' ? '' : reading(r.value)])),
  })
  submitted.value = false
  serverErrors.value = {}
  serverError.value = null
  editor.value = true
}

const errors = computed(() => {
  const e: Record<string, string> = {}
  if (!form.actDate) e.actDate = 'Укажите дату акта'
  form.inventory.forEach((r, i) => {
    if (!r.name.trim() || !r.condition.trim() || !r.quantity || r.quantity < 1)
      e[`inventory[${i}]`] = 'Заполните наименование, количество и состояние'
  })
  for (const [id, v] of Object.entries(form.readings)) {
    if (v.trim() && !/^\d{1,9}([.,]\d{1,3})?$/.test(v.trim())) e[id] = 'Число, не более 3 знаков после запятой'
  }
  return e
})
const showErr = (k: string) => serverErrors.value[k] ?? (submitted.value ? errors.value[k] : undefined)
const readingErr = (i: number, meterId: string) =>
  serverErrors.value[`readings[${i}].value`] ?? (submitted.value ? errors.value[meterId] : undefined)

async function save() {
  submitted.value = true
  serverErrors.value = {}
  serverError.value = null
  if (Object.keys(errors.value).length || !current.value) return
  const readings = current.value.readings
    .map((r) => ({ meterId: r.meterId, value: (form.readings[r.meterId] ?? '').trim().replace(',', '.') }))
    .filter((r) => r.value !== '')
  try {
    const a = await m.updateAct.mutateAsync({
      id: current.value.id,
      body: {
        actDate: form.actDate!,
        keysCount: form.keysCount ?? 0,
        conditionNotes: form.conditionNotes.trim() || null,
        damages: current.value.kind === 'return' ? form.damages.trim() || null : null,
        inventory: form.inventory.map((r) => ({
          name: r.name.trim(),
          quantity: r.quantity ?? 1,
          condition: r.condition.trim(),
          note: r.note.trim() || null,
        })),
        readings,
      },
    })
    current.value = a
    editor.value = false
    toast.add({ severity: 'success', summary: 'Акт сохранён', life: 3000 })
  } catch (err) {
    const p = parseProblem(err)
    // Индексы показаний в ответе — по порядку отправленных строк.
    const mapped: Record<string, string> = {}
    for (const [k, v] of Object.entries(p.fieldErrors)) {
      const mm = /^readings\[(\d+)\]\.value$/.exec(k)
      if (mm) {
        const r = readings[Number(mm[1])]
        const idx = current.value.readings.findIndex((x) => x.meterId === r?.meterId)
        mapped[`readings[${idx}].value`] = v
      } else mapped[k] = v
    }
    serverErrors.value = mapped
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}

// «Подписан».
const signDialog = ref(false)
const signAct = ref<Act | null>(null)
const signedOn = ref<string | null>(null)
const signError = ref<string | null>(null)
function openSign(a: Act) {
  signAct.value = a
  signedOn.value = todayIso()
  signError.value = null
  signDialog.value = true
}
async function sign() {
  if (!signedOn.value || !signAct.value) {
    signError.value = 'Укажите дату подписания'
    return
  }
  try {
    await m.signAct.mutateAsync({ id: signAct.value.id, signedOn: signedOn.value })
    signDialog.value = false
    toast.add({ severity: 'success', summary: 'Акт подписан, показания приняты', life: 3000 })
  } catch (err) {
    signError.value = parseProblem(err).message
  }
}

const filled = (a: Act) => a.readings.filter((r) => r.value != null).length
</script>

<template>
  <div>
    <Skeleton v-if="acts.isPending.value" height="8rem" />
    <div v-else class="cards">
      <Card v-for="a in [handover, ret].filter((x): x is Act => !!x)" :key="a.id">
        <template #title>{{ actKindLabels[a.kind] }}</template>
        <template #content>
          <dl class="props">
            <dt>Статус</dt>
            <dd>
              <Tag v-if="a.signed" :value="`подписан ${formatDate(a.signedOn)}`" severity="success" />
              <Tag v-else value="черновик" severity="warn" />
            </dd>
            <dt>Дата акта</dt>
            <dd>{{ formatDate(a.actDate) }}</dd>
            <dt>Опись</dt>
            <dd>{{ a.inventory.length }} строк</dd>
            <dt>Ключей</dt>
            <dd>{{ a.keysCount }}</dd>
            <dt>Показания</dt>
            <dd>{{ filled(a) }} из {{ a.readings.length }} счётчиков</dd>
          </dl>
          <div class="actions actions--start">
            <Button v-if="!a.signed" label="Заполнить" icon="pi pi-pencil" @click="openEditor(a)" />
            <Button v-else label="Открыть" icon="pi pi-eye" outlined @click="openEditor(a)" />
            <Button label="PDF" icon="pi pi-file-pdf" outlined :loading="docs.act.isPending.value" @click="actPdf(a)" />
            <Button v-if="!a.signed" label="Подписан" icon="pi pi-check" outlined @click="openSign(a)" />
          </div>
        </template>
      </Card>
      <Card v-if="canCreateReturn">
        <template #title>Акт возврата</template>
        <template #content>
          <p class="hint">Опись копируется из акта приёма-передачи.</p>
          <Button
            label="Создать акт возврата"
            icon="pi pi-plus"
            :loading="m.createReturnAct.isPending.value"
            @click="createReturn"
          />
        </template>
      </Card>
      <p v-if="!handover && contract.status === 'draft'" class="hint">
        Акт приёма-передачи создаётся при вводе договора в действие.
      </p>
    </div>

    <Dialog
      v-model:visible="editor"
      modal
      maximizable
      :header="current ? `${actKindLabels[current.kind]} — договор № ${contract.number}` : ''"
      :style="{ width: '1000px' }"
      :breakpoints="{ '1040px': '100vw' }"
    >
      <form v-if="current" class="form" novalidate @submit.prevent="save">
        <fieldset :disabled="current.signed" class="plain">
          <div class="form-grid">
            <div class="field">
              <label for="act-date">Дата акта *</label>
              <DateInput v-model="form.actDate" input-id="act-date" :invalid="!!showErr('actDate')" />
              <small v-if="showErr('actDate')" class="error">{{ showErr('actDate') }}</small>
            </div>
            <div class="field">
              <label for="act-keys">Ключей</label>
              <InputNumber v-model="form.keysCount" input-id="act-keys" :min="0" :max="50" fluid />
            </div>
            <div class="field field--wide">
              <label for="act-cond">Состояние помещения</label>
              <Textarea id="act-cond" v-model="form.conditionNotes" rows="2" auto-resize fluid />
            </div>
            <div v-if="current.kind === 'return'" class="field field--wide">
              <label for="act-dmg">Выявленные повреждения</label>
              <Textarea id="act-dmg" v-model="form.damages" rows="2" auto-resize fluid />
            </div>
          </div>

          <h3 class="subtitle">Опись имущества</h3>
          <div v-for="(r, i) in form.inventory" :key="i" class="inv-row">
            <InputText
              v-model="r.name"
              placeholder="Наименование"
              :aria-label="`Наименование, строка ${i + 1}`"
              class="inv-name"
            />
            <InputNumber
              v-model="r.quantity"
              :min="1"
              :aria-label="`Количество, строка ${i + 1}`"
              input-class="inv-qty"
            />
            <InputText v-model="r.condition" placeholder="Состояние" :aria-label="`Состояние, строка ${i + 1}`" />
            <InputText v-model="r.note" placeholder="Примечание" :aria-label="`Примечание, строка ${i + 1}`" />
            <Button
              v-if="!current.signed"
              type="button"
              icon="pi pi-trash"
              text
              severity="danger"
              :aria-label="`Удалить строку ${i + 1}`"
              @click="form.inventory.splice(i, 1)"
            />
            <small v-if="showErr(`inventory[${i}]`)" class="error inv-error">{{ showErr(`inventory[${i}]`) }}</small>
          </div>
          <Button
            v-if="!current.signed"
            type="button"
            label="Добавить строку"
            icon="pi pi-plus"
            text
            @click="form.inventory.push({ name: '', quantity: 1, condition: 'исправно', note: '' })"
          />

          <h3 class="subtitle">Показания счётчиков</h3>
          <p v-if="!current.readings.length" class="hint">У объекта нет счётчиков.</p>
          <div v-for="(r, i) in current.readings" :key="r.meterId" class="field reading">
            <label :for="`act-r-${r.meterId}`">
              {{ meterTypeLabels[r.meterType] }}<template v-if="r.serialNumber"> № {{ r.serialNumber }}</template>
            </label>
            <InputText
              :id="`act-r-${r.meterId}`"
              v-model="form.readings[r.meterId]"
              inputmode="decimal"
              :invalid="!!readingErr(i, r.meterId)"
            />
            <small v-if="readingErr(i, r.meterId)" class="error">{{ readingErr(i, r.meterId) }}</small>
            <small v-else-if="r.previousValue" class="hint"
              >предыдущее: {{ reading(r.previousValue) }} на {{ formatDate(r.previousDate) }}</small
            >
          </div>
        </fieldset>
        <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
        <div class="actions">
          <Button type="button" :label="current.signed ? 'Закрыть' : 'Отмена'" outlined @click="editor = false" />
          <Button v-if="!current.signed" type="submit" label="Сохранить акт" :loading="m.updateAct.isPending.value" />
        </div>
      </form>
    </Dialog>

    <Dialog
      v-model:visible="signDialog"
      modal
      header="Акт подписан"
      :style="{ width: '440px' }"
      :breakpoints="{ '480px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="sign">
        <div class="field">
          <label for="act-signed">Дата подписания *</label>
          <DateInput v-model="signedOn" input-id="act-signed" :max-date="today()" />
        </div>
        <p class="hint">После подписания акт не редактируется, показания счётчиков принимаются.</p>
        <Message v-if="signError" severity="error" :closable="false">{{ signError }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="signDialog = false" />
          <Button type="submit" label="Подписан" :loading="m.signAct.isPending.value" />
        </div>
      </form>
    </Dialog>
  </div>
</template>

<style scoped>
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 1rem;
}
.props {
  display: grid;
  grid-template-columns: 120px 1fr;
  gap: 0.375rem 1rem;
  margin: 0 0 1rem;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
}
.actions--start {
  justify-content: flex-start;
}
.plain {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.subtitle {
  font-size: 1rem;
  margin: 1.25rem 0 0.5rem;
}
.inv-row {
  display: grid;
  grid-template-columns: 2fr 90px 1.2fr 1.2fr auto;
  gap: 0.5rem;
  align-items: center;
  margin-bottom: 0.5rem;
}
.inv-row :deep(.inv-qty) {
  width: 100%;
}
.inv-error {
  grid-column: 1 / -1;
}
.reading {
  max-width: 360px;
  margin-bottom: 0.75rem;
}
@media (max-width: 720px) {
  .inv-row {
    grid-template-columns: 1fr 80px;
  }
}
</style>
