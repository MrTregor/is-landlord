<script setup lang="ts">
// Вкладка «Условия» карточки договора (14 Экраны собственника, раздел 4.3, вкладка 1; 05 разд. 4.5).
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useContractMutations, type ContractDetail } from '@/api/queries/contracts'
import DateInput from '@/components/DateInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import { formatDate } from '@/utils/date'
import { utilitiesModeLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { useSaveState } from '@/utils/saveState'
import { today } from '@/utils/today'
import { conditionsSchema, fieldErrors, pctToApi, pctToText, type OccupantRow } from '../schemas'

const props = defineProps<{ contract: ContractDetail }>()
const toast = useToast()
const m = useContractMutations(() => props.contract.id)
const c = computed(() => props.contract)
const live = computed(() => c.value.status === 'active' || c.value.status === 'extended')
const editable = computed(() => c.value.status !== 'terminated' && c.value.status !== 'finished')

function fail(err: unknown) {
  toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
}

// Пени и индексация: правка с обязательной причиной.
const penDialog = ref(false)
const pen = reactive({
  penaltyEnabled: false,
  penaltyRatePct: '',
  penaltyGraceDays: 0 as number | null,
  penaltyCapPct: '',
  penaltyOnUtilities: false,
  indexationEnabled: false,
  indexationPct: '',
  indexationPeriodMonths: 12 as number | null,
  reason: '',
})
const penSubmitted = ref(false)
const penServer = ref<string | null>(null)
function openPenalty() {
  Object.assign(pen, {
    penaltyEnabled: c.value.penaltyEnabled,
    penaltyRatePct: pctToText(c.value.penaltyRatePct) || '0,1',
    penaltyGraceDays: c.value.penaltyGraceDays,
    penaltyCapPct: pctToText(c.value.penaltyCapPct),
    penaltyOnUtilities: c.value.penaltyOnUtilities,
    indexationEnabled: c.value.indexationEnabled,
    indexationPct: pctToText(c.value.indexationPct) || '5',
    indexationPeriodMonths: c.value.indexationPeriodMonths,
    reason: '',
  })
  penSubmitted.value = false
  penServer.value = null
  penDialog.value = true
}
const penErrors = computed(() => {
  const e = fieldErrors(conditionsSchema, { ...pen, utilitiesMode: 'included', utilitiesFixedKop: 0 })
  if (pen.reason.trim().length < 5) e.reason = 'Причина — не короче 5 символов'
  return e
})
async function savePenalty() {
  penSubmitted.value = true
  if (Object.keys(penErrors.value).length) return
  penServer.value = null
  try {
    await m.patch.mutateAsync({
      penaltyEnabled: pen.penaltyEnabled,
      penaltyRatePct: pen.penaltyEnabled ? pctToApi(pen.penaltyRatePct) : null,
      penaltyGraceDays: pen.penaltyGraceDays ?? 0,
      penaltyCapPct: pen.penaltyEnabled ? pctToApi(pen.penaltyCapPct) : null,
      penaltyOnUtilities: pen.penaltyEnabled && pen.penaltyOnUtilities,
      indexationEnabled: pen.indexationEnabled,
      indexationPct: pen.indexationEnabled ? pctToApi(pen.indexationPct) : null,
      indexationPeriodMonths: pen.indexationPeriodMonths ?? 12,
      reason: pen.reason.trim(),
    })
    penDialog.value = false
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
  } catch (err) {
    penServer.value = parseProblem(err).message
  }
}

// Особые условия и заметки.
const textDialog = ref(false)
const texts = reactive({ specialTerms: '', notes: '' })
function openTexts() {
  texts.specialTerms = c.value.specialTerms ?? ''
  texts.notes = c.value.notes ?? ''
  textDialog.value = true
}
async function saveTexts() {
  try {
    await m.patch.mutateAsync({ specialTerms: texts.specialTerms.trim(), notes: texts.notes.trim() })
    textDialog.value = false
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
  } catch (err) {
    fail(err)
  }
}

// Регистрация договора.
const reg = reactive({ doneOn: null as string | null, number: '' })
const regState = useSaveState(() => reg)
watch(
  c,
  (v) => {
    reg.doneOn = v.registrationDoneOn
    reg.number = v.registrationNumber ?? ''
  },
  { immediate: true },
)
async function saveRegistration() {
  regState.reset()
  try {
    await m.patch.mutateAsync({ registrationDoneOn: reg.doneOn, registrationNumber: reg.number.trim() })
    await nextTick()
    regState.saved()
    toast.add({ severity: 'success', summary: 'Регистрация отмечена', life: 3000 })
  } catch (err) {
    fail(err)
    regState.failed()
  }
}

// Проживающие.
const occupants = ref<OccupantRow[]>([])
const occSubmitted = ref(false)
const occState = useSaveState(() => occupants.value, 'Сохранить проживающих')
watch(
  () => c.value.occupants,
  (list) => {
    occupants.value = list.map((o) => ({ fullName: o.fullName, birthDate: o.birthDate, relation: o.relation ?? '' }))
  },
  { immediate: true },
)
async function saveOccupants() {
  occSubmitted.value = true
  occState.reset()
  if (occupants.value.some((o) => !o.fullName.trim())) return
  try {
    await m.occupants.mutateAsync({
      occupants: occupants.value.map((o) => ({
        fullName: o.fullName.trim(),
        birthDate: o.birthDate,
        relation: o.relation.trim() || null,
      })),
    })
    occSubmitted.value = false
    await nextTick()
    occState.saved()
    toast.add({ severity: 'success', summary: 'Проживающие сохранены', life: 3000 })
  } catch (err) {
    fail(err)
    occState.failed()
  }
}

const pctText = (v: string | null) => pctToText(v)
</script>

<template>
  <div class="tab">
    <div class="blocks">
      <Card>
        <template #title>Условия</template>
        <template #content>
          <dl class="props">
            <dt>Срок</dt>
            <dd>{{ formatDate(c.startDate) }} – {{ formatDate(c.endDate) }}</dd>
            <template v-if="c.terminationDate">
              <dt>Последний день пользования</dt>
              <dd>{{ formatDate(c.terminationDate) }}</dd>
            </template>
            <dt>Подписан</dt>
            <dd>{{ formatDate(c.concludedOn) || '—' }}</dd>
            <dt>Плата в месяц</dt>
            <dd><MoneyText :kop="c.currentTerms.rentKop" /></dd>
            <dt>День оплаты</dt>
            <dd>{{ c.currentTerms.paymentDay }}</dd>
            <dt>Срок первой оплаты</dt>
            <dd>{{ formatDate(c.firstDueDate) }}</dd>
            <dt>Обеспечительный платёж</dt>
            <dd><MoneyText :kop="c.depositKop" /></dd>
            <dt>ЖКУ</dt>
            <dd>
              {{ utilitiesModeLabels[c.utilitiesMode] }}
              <template v-if="c.utilitiesMode !== 'included' && c.currentTerms.utilitiesFixedKop">
                , фиксированная часть <MoneyText :kop="c.currentTerms.utilitiesFixedKop" />
              </template>
            </dd>
          </dl>
        </template>
      </Card>

      <Card>
        <template #title>
          <div class="title-row">
            Пени и индексация
            <Button v-if="live" label="Изменить" icon="pi pi-pencil" size="small" text @click="openPenalty" />
          </div>
        </template>
        <template #content>
          <dl class="props">
            <dt>Пени</dt>
            <dd>
              <template v-if="c.penaltyEnabled">
                {{ pctText(c.penaltyRatePct) }} % в день, льгота {{ c.penaltyGraceDays }} дн.<template
                  v-if="c.penaltyCapPct"
                  >, не более {{ pctText(c.penaltyCapPct) }} %</template
                ><template v-if="c.penaltyOnUtilities">, в т.ч. на ЖКУ</template>
              </template>
              <template v-else>не начисляются</template>
            </dd>
            <dt>Индексация</dt>
            <dd>
              <template v-if="c.indexationEnabled">
                {{ pctText(c.indexationPct) }} % раз в {{ c.indexationPeriodMonths }} мес.
              </template>
              <template v-else>нет</template>
            </dd>
            <template v-if="c.indexationEnabled && c.nextIndexationOn">
              <dt>Следующая индексация</dt>
              <dd>{{ formatDate(c.nextIndexationOn) }}</dd>
            </template>
          </dl>
        </template>
      </Card>

      <Card v-if="c.registrationRequired">
        <template #title>Регистрация</template>
        <template #content>
          <div class="form">
            <div class="field">
              <label for="r-date">Дата регистрации</label>
              <DateInput v-model="reg.doneOn" input-id="r-date" :max-date="today()" />
            </div>
            <div class="field">
              <label for="r-number">Номер регистрации</label>
              <InputText id="r-number" v-model="reg.number" fluid :disabled="!live" />
            </div>
            <div v-if="live" class="actions">
              <Button
                :label="regState.button.value.label"
                :icon="regState.button.value.icon"
                :severity="regState.button.value.severity"
                :loading="m.patch.isPending.value"
                @click="saveRegistration"
              />
            </div>
          </div>
        </template>
      </Card>
    </div>

    <Card class="section">
      <template #title>История условий</template>
      <template #content>
        <DataTable :value="c.termsHistory" size="small" data-key="version">
          <Column header="Версия" field="version" />
          <Column header="С даты">
            <template #body="{ data }">{{ formatDate(data.effectiveFrom) }}</template>
          </Column>
          <Column header="Плата">
            <template #body="{ data }"><MoneyText :kop="data.rentKop" /></template>
          </Column>
          <Column header="День оплаты" field="paymentDay" />
          <Column header="Фиксированная часть ЖКУ">
            <template #body="{ data }"><MoneyText :kop="data.utilitiesFixedKop" /></template>
          </Column>
          <Column header="Основание">
            <template #body="{ data }">{{ data.amendmentId ? 'Допсоглашение' : 'Договор' }}</template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Card class="section">
      <template #title>
        <div class="title-row">
          Особые условия и заметки
          <Button v-if="live" label="Изменить" icon="pi pi-pencil" size="small" text @click="openTexts" />
        </div>
      </template>
      <template #content>
        <dl class="props">
          <dt>Особые условия</dt>
          <dd class="pre">{{ c.specialTerms || '—' }}</dd>
          <dt>Заметки</dt>
          <dd class="pre">{{ c.notes || '—' }}</dd>
        </dl>
      </template>
    </Card>

    <Card class="section">
      <template #title>Проживающие</template>
      <template #content>
        <p v-if="!occupants.length" class="hint">Проживающих нет</p>
        <div v-for="(o, i) in occupants" :key="i" class="occupant">
          <div class="field grow">
            <label :for="`o-name-${i}`">ФИО *</label>
            <InputText
              :id="`o-name-${i}`"
              v-model="o.fullName"
              :disabled="!editable"
              :invalid="occSubmitted && !o.fullName.trim()"
              fluid
            />
            <small v-if="occSubmitted && !o.fullName.trim()" class="error">Укажите ФИО проживающего</small>
          </div>
          <div class="field">
            <label :for="`o-birth-${i}`">Дата рождения</label>
            <DateInput v-model="o.birthDate" :input-id="`o-birth-${i}`" :max-date="today()" />
          </div>
          <div class="field">
            <label :for="`o-rel-${i}`">Кем приходится</label>
            <InputText :id="`o-rel-${i}`" v-model="o.relation" :disabled="!editable" fluid />
          </div>
          <Button
            v-if="editable"
            icon="pi pi-trash"
            text
            severity="danger"
            aria-label="Удалить проживающего"
            @click="occupants.splice(i, 1)"
          />
        </div>
        <div v-if="editable" class="actions actions--start">
          <Button
            label="Добавить"
            icon="pi pi-plus"
            text
            @click="occupants.push({ fullName: '', birthDate: null, relation: '' })"
          />
          <Button
            :label="occState.button.value.label"
            :icon="occState.button.value.icon"
            :severity="occState.button.value.severity"
            :loading="m.occupants.isPending.value"
            @click="saveOccupants"
          />
        </div>
      </template>
    </Card>

    <Dialog
      v-model:visible="penDialog"
      modal
      header="Пени и индексация"
      :style="{ width: '600px' }"
      :breakpoints="{ '640px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="savePenalty">
        <div class="field field--check">
          <ToggleSwitch v-model="pen.penaltyEnabled" input-id="p-on" />
          <label for="p-on">Начислять пени</label>
        </div>
        <div v-if="pen.penaltyEnabled" class="form-grid">
          <div class="field">
            <label for="p-rate">Ставка, % в день *</label>
            <InputText
              id="p-rate"
              v-model="pen.penaltyRatePct"
              :invalid="penSubmitted && !!penErrors.penaltyRatePct"
              fluid
            />
            <small v-if="penSubmitted && penErrors.penaltyRatePct" class="error">{{ penErrors.penaltyRatePct }}</small>
          </div>
          <div class="field">
            <label for="p-grace">Льготных дней</label>
            <InputNumber v-model="pen.penaltyGraceDays" input-id="p-grace" :min="0" :max="30" fluid />
          </div>
          <div class="field">
            <label for="p-cap">Предел, % от суммы</label>
            <InputText
              id="p-cap"
              v-model="pen.penaltyCapPct"
              :invalid="penSubmitted && !!penErrors.penaltyCapPct"
              fluid
            />
            <small v-if="penSubmitted && penErrors.penaltyCapPct" class="error">{{ penErrors.penaltyCapPct }}</small>
          </div>
          <div class="field field--check">
            <Checkbox v-model="pen.penaltyOnUtilities" input-id="p-util" binary />
            <label for="p-util">Начислять пени на ЖКУ</label>
          </div>
        </div>
        <div class="field field--check">
          <ToggleSwitch v-model="pen.indexationEnabled" input-id="i-on" />
          <label for="i-on">Индексировать плату</label>
        </div>
        <div v-if="pen.indexationEnabled" class="form-grid">
          <div class="field">
            <label for="i-pct">Процент *</label>
            <InputText
              id="i-pct"
              v-model="pen.indexationPct"
              :invalid="penSubmitted && !!penErrors.indexationPct"
              fluid
            />
            <small v-if="penSubmitted && penErrors.indexationPct" class="error">{{ penErrors.indexationPct }}</small>
          </div>
          <div class="field">
            <label for="i-period">Период, мес.</label>
            <InputNumber v-model="pen.indexationPeriodMonths" input-id="i-period" :min="12" :max="120" fluid />
          </div>
        </div>
        <div class="field">
          <label for="p-reason">Причина изменения *</label>
          <Textarea
            id="p-reason"
            v-model="pen.reason"
            rows="2"
            auto-resize
            :invalid="penSubmitted && !!penErrors.reason"
            fluid
          />
          <small v-if="penSubmitted && penErrors.reason" class="error">{{ penErrors.reason }}</small>
        </div>
        <Message v-if="penServer" severity="error" :closable="false">{{ penServer }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="penDialog = false" />
          <Button type="submit" label="Сохранить" :loading="m.patch.isPending.value" />
        </div>
      </form>
    </Dialog>

    <Dialog
      v-model:visible="textDialog"
      modal
      header="Особые условия и заметки"
      :style="{ width: '600px' }"
      :breakpoints="{ '640px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="saveTexts">
        <div class="field">
          <label for="s-special">Особые условия</label>
          <Textarea id="s-special" v-model="texts.specialTerms" rows="4" auto-resize fluid />
        </div>
        <div class="field">
          <label for="s-notes">Заметки</label>
          <Textarea id="s-notes" v-model="texts.notes" rows="3" auto-resize fluid />
        </div>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="textDialog = false" />
          <Button type="submit" label="Сохранить" :loading="m.patch.isPending.value" />
        </div>
      </form>
    </Dialog>
  </div>
</template>

<style scoped>
.blocks {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 1rem;
}
.section {
  margin-top: 1rem;
}
.props {
  display: grid;
  grid-template-columns: minmax(140px, 190px) 1fr;
  gap: 0.375rem 1rem;
  margin: 0;
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
.title-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
}
.occupant {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 0.75rem;
  margin-bottom: 0.75rem;
}
.grow {
  flex: 1 1 260px;
}
.actions--start {
  justify-content: flex-start;
}
</style>
