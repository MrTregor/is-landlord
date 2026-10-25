<script setup lang="ts">
// Мастер создания договора /contracts/new и правка черновика /contracts/:id/edit (14 Экраны собственника, раздел 4.2).
// Черновик сохраняется при каждом «Далее», начиная с шага «Срок и плата» (до него у договора нет обязательных условий).
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { useContract, useContractMutations, useCreateContract, type ContractDetail } from '@/api/queries/contracts'
import { documentPdfUrl, useDocumentMutations } from '@/api/queries/documents'
import { useProperties } from '@/api/queries/properties'
import { useTenants } from '@/api/queries/tenants'
import DateInput from '@/components/DateInput.vue'
import MoneyInput from '@/components/MoneyInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import { formatDate, fromIsoDate } from '@/utils/date'
import { options, propertyStatusLabels, utilitiesModeLabels } from '@/utils/labels'
import { openInNewTab } from '@/utils/pdf'
import { parseProblem } from '@/utils/problem'
import { today } from '@/utils/today'
import TenantFormPage from '@/modules/tenants/pages/TenantFormPage.vue'
import ActivateDialog from '../components/ActivateDialog.vue'
import {
  conditionsSchema,
  defaultPaymentDay,
  emptyContractForm,
  endAfter,
  fieldErrors,
  firstIndexation,
  fromDetail,
  isShortTerm,
  legalBasis,
  legalBasisTitle,
  occupantsSchema,
  partiesSchema,
  termsSchema,
  toInput,
  wizardWarnings,
  type ContractForm,
} from '../schemas'

const route = useRoute()
const router = useRouter()
const toast = useToast()

const routeId = computed(() => (route.params.id as string | undefined) ?? '')
const contractId = ref(routeId.value)
const existing = useContract(routeId)
const create = useCreateContract()
const m = useContractMutations(contractId)
const saved = ref<ContractDetail | null>(null)

const form = reactive<ContractForm>(emptyContractForm((route.query.propertyId as string | undefined) ?? null))
const loaded = ref(!routeId.value)
// Объявлен до watch с immediate: договор из кэша (переход с карточки) приходит сразу, во время setup.
const firstDueTouched = ref(false)
watch(
  () => existing.data.value,
  (d) => {
    if (!d || loaded.value) return
    if (d.status !== 'draft') {
      void router.replace(`/contracts/${d.id}`)
      return
    }
    Object.assign(form, fromDetail(d))
    saved.value = d
    firstDueTouched.value = true
    loaded.value = true
  },
  { immediate: true },
)

// Справочники: неархивные объекты и необезличенные наниматели.
const properties = useProperties({ pageSize: 100 })
const tenantSearch = ref('')
const tenants = useTenants(() => ({ pageSize: 100, q: tenantSearch.value.trim() || undefined }))
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((p) => ({
    value: p.id,
    label: `${p.code} — ${p.address} — ${propertyStatusLabels[p.status]}`,
    kind: p.kind,
    fiscalMode: p.fiscalMode,
  })),
)
const tenantOptions = computed(() => {
  const list = (tenants.data.value?.items ?? []).map((t) => ({ value: t.id, label: t.displayName, kind: t.kind }))
  if (saved.value && !list.some((t) => t.value === saved.value!.tenant.id)) {
    list.unshift({ value: saved.value.tenant.id, label: saved.value.tenant.displayName, kind: saved.value.tenant.kind })
  }
  return list
})
const property = computed(() => propertyOptions.value.find((p) => p.value === form.propertyId) ?? null)
const tenant = computed(() => tenantOptions.value.find((t) => t.value === form.tenantId) ?? null)
const basis = computed(() =>
  property.value && tenant.value ? legalBasis(property.value.kind, tenant.value.kind) : null,
)

// «Новый наниматель» — форма 3.2 в диалоге.
const tenantDialog = ref(false)
function tenantCreated(id: string) {
  tenantDialog.value = false
  form.tenantId = id
  touched.add('tenantId')
}

// Значения по умолчанию шага 2.
watch(
  () => form.startDate,
  (s) => {
    if (!s) return
    if (form.paymentDay == null) form.paymentDay = defaultPaymentDay(s)
    if (!firstDueTouched.value) form.firstDueDate = s
  },
)
function quickTerm(months: number) {
  if (!form.startDate) return
  form.endDate = endAfter(form.startDate, months)
  touched.add('endDate')
}

// Шаги и ошибки (показываются после ухода с поля или попытки перейти дальше).
const step = ref('1')
const touched = reactive(new Set<string>())
const attempted = reactive(new Set<string>())
const serverErrors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)

const stepErrors = computed<Record<string, Record<string, string>>>(() => ({
  '1': fieldErrors(partiesSchema, form),
  '2': fieldErrors(termsSchema(basis.value), form),
  '3': fieldErrors(conditionsSchema, form),
  '4': fieldErrors(occupantsSchema, form),
}))
function err(stepNo: string, field: string): string | undefined {
  if (serverErrors.value[field]) return serverErrors.value[field]
  if (!attempted.has(stepNo) && !touched.has(field)) return undefined
  return stepErrors.value[stepNo]?.[field]
}
const touch = (f: string) => touched.add(f)

const fieldStep: Record<string, string> = {
  propertyId: '1',
  tenantId: '1',
  startDate: '2',
  endDate: '2',
  rentKop: '2',
  paymentDay: '2',
  firstDueDate: '2',
  depositKop: '2',
  utilitiesMode: '3',
  utilitiesFixedKop: '3',
  penaltyRatePct: '3',
  penaltyGraceDays: '3',
  penaltyCapPct: '3',
  indexationPct: '3',
  indexationPeriodMonths: '3',
}

const saving = ref(false)
/** Создать или обновить черновик; true — успешно. */
async function persist(): Promise<boolean> {
  serverErrors.value = {}
  serverError.value = null
  saving.value = true
  try {
    const body = toInput(form)
    saved.value = contractId.value ? await m.update.mutateAsync(body) : await create.mutateAsync(body)
    contractId.value = saved.value.id
    return true
  } catch (e) {
    const p = parseProblem(e)
    serverErrors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
    const first = Object.keys(p.fieldErrors).map((f) => fieldStep[f.split('[')[0]!] ?? '4')[0]
    if (first) step.value = first
    return false
  } finally {
    saving.value = false
  }
}

async function next(from: string) {
  for (const s of ['1', '2', '3', '4'].filter((s) => s <= from)) attempted.add(s)
  const bad = ['1', '2', '3', '4'].find((s) => s <= from && Object.keys(stepErrors.value[s] ?? {}).length)
  if (bad) {
    step.value = bad
    return
  }
  if (from >= '2' && !(await persist())) return
  step.value = String(Number(from) + 1)
}

async function saveDraft() {
  for (const s of ['1', '2', '3', '4']) attempted.add(s)
  const bad = ['1', '2', '3', '4'].find((s) => Object.keys(stepErrors.value[s] ?? {}).length)
  if (bad) {
    step.value = bad
    return
  }
  if (await persist()) {
    toast.add({ severity: 'success', summary: 'Черновик сохранён', life: 3000 })
    await router.push(`/contracts/${contractId.value}`)
  }
}

const docs = useDocumentMutations()
/** Сохранить черновик и открыть проект договора в новой вкладке. */
async function projectPdf() {
  try {
    await openInNewTab(async () => {
      if (!(await persist())) return null
      return documentPdfUrl((await docs.contract.mutateAsync(contractId.value!)).id)
    })
  } catch (e) {
    serverError.value = parseProblem(e).message
  }
}

const activateDialog = ref(false)
async function openActivate() {
  if (await persist()) activateDialog.value = true
}

const warnings = computed(() => wizardWarnings(form, property.value?.kind ?? null, property.value?.fiscalMode ?? null))
const indexationHint = computed(() =>
  form.startDate && form.indexationPeriodMonths && form.indexationPeriodMonths >= 12
    ? formatDate(firstIndexation(form.startDate, form.indexationPeriodMonths))
    : null,
)
const shortTerm = computed(
  () =>
    !!basis.value &&
    !!form.startDate &&
    !!form.endDate &&
    form.endDate > form.startDate &&
    isShortTerm(basis.value, form.startDate, form.endDate),
)

function addOccupant() {
  form.occupants.push({ fullName: '', birthDate: null, relation: '' })
}
const steps = [
  { value: '1', label: 'Стороны' },
  { value: '2', label: 'Срок и плата' },
  { value: '3', label: 'ЖКУ, пени, индексация' },
  { value: '4', label: 'Проживающие и особые условия' },
  { value: '5', label: 'Проверка' },
]
</script>

<template>
  <div>
    <PageHeader :title="routeId ? `Изменение черновика № ${existing.data.value?.number ?? ''}` : 'Новый договор'">
      <template v-if="saved" #actions>
        <span class="hint">Черновик № {{ saved.number }} сохранён</span>
      </template>
    </PageHeader>
    <Skeleton v-if="!loaded" height="20rem" />
    <Stepper v-else v-model:value="step" class="card">
      <StepList>
        <Step v-for="s in steps" :key="s.value" :value="s.value">{{ s.label }}</Step>
      </StepList>
      <StepPanels>
        <!-- 1. Стороны -->
        <StepPanel value="1">
          <div class="form">
            <div class="form-grid">
              <div class="field field--wide">
                <label for="w-property">Объект *</label>
                <Select
                  v-model="form.propertyId"
                  input-id="w-property"
                  :options="propertyOptions"
                  option-label="label"
                  option-value="value"
                  filter
                  placeholder="Выберите объект"
                  :invalid="!!err('1', 'propertyId')"
                  fluid
                  @hide="touch('propertyId')"
                />
                <small v-if="err('1', 'propertyId')" class="error">{{ err('1', 'propertyId') }}</small>
              </div>
              <div class="field field--wide">
                <label for="w-tenant">Наниматель *</label>
                <div class="row">
                  <Select
                    v-model="form.tenantId"
                    input-id="w-tenant"
                    :options="tenantOptions"
                    option-label="label"
                    option-value="value"
                    filter
                    :filter-fields="['label']"
                    placeholder="Начните вводить ФИО или наименование"
                    :invalid="!!err('1', 'tenantId')"
                    class="grow"
                    @filter="(e: { value: string }) => (tenantSearch = e.value)"
                    @hide="touch('tenantId')"
                  />
                  <Button
                    type="button"
                    label="Новый наниматель"
                    icon="pi pi-user-plus"
                    outlined
                    @click="tenantDialog = true"
                  />
                </div>
                <small v-if="err('1', 'tenantId')" class="error">{{ err('1', 'tenantId') }}</small>
              </div>
            </div>
            <Message v-if="basis" severity="info" :closable="false">
              {{ legalBasisTitle[basis] }}<template v-if="shortTerm"> — краткосрочный наём</template>
            </Message>
            <div class="actions">
              <Button type="button" label="Отмена" outlined @click="router.back()" />
              <Button type="button" label="Далее" icon="pi pi-arrow-right" icon-pos="right" @click="next('1')" />
            </div>
          </div>
        </StepPanel>

        <!-- 2. Срок и плата -->
        <StepPanel value="2">
          <div class="form">
            <div class="form-grid">
              <div class="field">
                <label for="w-start">Дата начала *</label>
                <DateInput
                  v-model="form.startDate"
                  input-id="w-start"
                  :invalid="!!err('2', 'startDate')"
                  @blur="touch('startDate')"
                />
                <small v-if="err('2', 'startDate')" class="error">{{ err('2', 'startDate') }}</small>
              </div>
              <div class="field">
                <label for="w-end">Дата окончания *</label>
                <DateInput
                  v-model="form.endDate"
                  input-id="w-end"
                  :min-date="form.startDate ? fromIsoDate(form.startDate) : undefined"
                  :invalid="!!err('2', 'endDate')"
                  @blur="touch('endDate')"
                />
                <div class="row">
                  <Button
                    type="button"
                    label="11 месяцев"
                    size="small"
                    text
                    :disabled="!form.startDate"
                    @click="quickTerm(11)"
                  />
                  <Button
                    type="button"
                    label="1 год"
                    size="small"
                    text
                    :disabled="!form.startDate"
                    @click="quickTerm(12)"
                  />
                </div>
                <small v-if="err('2', 'endDate')" class="error">{{ err('2', 'endDate') }}</small>
              </div>
              <div class="field">
                <label for="w-rent">Плата в месяц *</label>
                <MoneyInput
                  v-model="form.rentKop"
                  input-id="w-rent"
                  :invalid="!!err('2', 'rentKop')"
                  @blur="touch('rentKop')"
                />
                <small v-if="err('2', 'rentKop')" class="error">{{ err('2', 'rentKop') }}</small>
              </div>
              <div class="field">
                <label for="w-day">День оплаты *</label>
                <InputNumber
                  v-model="form.paymentDay"
                  input-id="w-day"
                  :min="1"
                  :max="28"
                  :use-grouping="false"
                  :invalid="!!err('2', 'paymentDay')"
                  fluid
                  @blur="touch('paymentDay')"
                />
                <small v-if="err('2', 'paymentDay')" class="error">{{ err('2', 'paymentDay') }}</small>
              </div>
              <div class="field">
                <label for="w-first">Срок первой оплаты *</label>
                <DateInput
                  v-model="form.firstDueDate"
                  input-id="w-first"
                  :invalid="!!err('2', 'firstDueDate')"
                  @blur="
                    () => {
                      firstDueTouched = true
                      touch('firstDueDate')
                    }
                  "
                />
                <small v-if="err('2', 'firstDueDate')" class="error">{{ err('2', 'firstDueDate') }}</small>
              </div>
              <div class="field">
                <label for="w-deposit">Обеспечительный платёж</label>
                <MoneyInput v-model="form.depositKop" input-id="w-deposit" :invalid="!!err('2', 'depositKop')" />
                <small v-if="err('2', 'depositKop')" class="error">{{ err('2', 'depositKop') }}</small>
              </div>
            </div>
            <Message v-for="w in warnings" :key="w" severity="warn" :closable="false">{{ w }}</Message>
            <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
            <div class="actions">
              <Button type="button" label="Назад" outlined @click="step = '1'" />
              <Button
                type="button"
                label="Далее"
                icon="pi pi-arrow-right"
                icon-pos="right"
                :loading="saving"
                @click="next('2')"
              />
            </div>
          </div>
        </StepPanel>

        <!-- 3. ЖКУ, пени, индексация -->
        <StepPanel value="3">
          <div class="form">
            <fieldset class="group">
              <legend>ЖКУ</legend>
              <div class="form-grid">
                <div class="field">
                  <label id="w-util-label">Порядок ЖКУ *</label>
                  <SelectButton
                    v-model="form.utilitiesMode"
                    :options="options(utilitiesModeLabels)"
                    option-label="label"
                    option-value="value"
                    :allow-empty="false"
                    aria-labelledby="w-util-label"
                  />
                </div>
                <div v-if="form.utilitiesMode !== 'included'" class="field">
                  <label for="w-fixed">Фиксированная часть ЖКУ{{ form.utilitiesMode === 'fixed' ? ' *' : '' }}</label>
                  <MoneyInput
                    v-model="form.utilitiesFixedKop"
                    input-id="w-fixed"
                    :invalid="!!err('3', 'utilitiesFixedKop')"
                    @blur="touch('utilitiesFixedKop')"
                  />
                  <small v-if="err('3', 'utilitiesFixedKop')" class="error">{{ err('3', 'utilitiesFixedKop') }}</small>
                </div>
              </div>
            </fieldset>
            <fieldset class="group">
              <legend>Пени</legend>
              <div class="field field--check">
                <ToggleSwitch v-model="form.penaltyEnabled" input-id="w-pen" />
                <label for="w-pen">Начислять пени за просрочку</label>
              </div>
              <div v-if="form.penaltyEnabled" class="form-grid">
                <div class="field">
                  <label for="w-rate">Ставка, % в день *</label>
                  <InputText
                    id="w-rate"
                    v-model="form.penaltyRatePct"
                    inputmode="decimal"
                    :invalid="!!err('3', 'penaltyRatePct')"
                    fluid
                    @blur="touch('penaltyRatePct')"
                  />
                  <small v-if="err('3', 'penaltyRatePct')" class="error">{{ err('3', 'penaltyRatePct') }}</small>
                </div>
                <div class="field">
                  <label for="w-grace">Льготных дней</label>
                  <InputNumber v-model="form.penaltyGraceDays" input-id="w-grace" :min="0" :max="30" fluid />
                </div>
                <div class="field">
                  <label for="w-cap">Предел, % от суммы</label>
                  <InputText
                    id="w-cap"
                    v-model="form.penaltyCapPct"
                    inputmode="decimal"
                    placeholder="без предела"
                    :invalid="!!err('3', 'penaltyCapPct')"
                    fluid
                    @blur="touch('penaltyCapPct')"
                  />
                  <small v-if="err('3', 'penaltyCapPct')" class="error">{{ err('3', 'penaltyCapPct') }}</small>
                </div>
                <div class="field field--check">
                  <Checkbox v-model="form.penaltyOnUtilities" input-id="w-pen-util" binary />
                  <label for="w-pen-util">Начислять пени на ЖКУ</label>
                </div>
              </div>
            </fieldset>
            <fieldset class="group">
              <legend>Индексация</legend>
              <div class="field field--check">
                <ToggleSwitch v-model="form.indexationEnabled" input-id="w-idx" />
                <label for="w-idx">Индексировать плату</label>
              </div>
              <div v-if="form.indexationEnabled" class="form-grid">
                <div class="field">
                  <label for="w-idx-pct">Процент *</label>
                  <InputText
                    id="w-idx-pct"
                    v-model="form.indexationPct"
                    inputmode="decimal"
                    :invalid="!!err('3', 'indexationPct')"
                    fluid
                    @blur="touch('indexationPct')"
                  />
                  <small v-if="err('3', 'indexationPct')" class="error">{{ err('3', 'indexationPct') }}</small>
                </div>
                <div class="field">
                  <label for="w-idx-period">Период, мес.</label>
                  <InputNumber
                    v-model="form.indexationPeriodMonths"
                    input-id="w-idx-period"
                    :min="12"
                    :max="120"
                    :invalid="!!err('3', 'indexationPeriodMonths')"
                    fluid
                    @blur="touch('indexationPeriodMonths')"
                  />
                  <small v-if="err('3', 'indexationPeriodMonths')" class="error">{{
                    err('3', 'indexationPeriodMonths')
                  }}</small>
                  <small v-else-if="indexationHint" class="hint"
                    >Первая индексация — не ранее {{ indexationHint }}</small
                  >
                </div>
              </div>
            </fieldset>
            <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
            <div class="actions">
              <Button type="button" label="Назад" outlined @click="step = '2'" />
              <Button
                type="button"
                label="Далее"
                icon="pi pi-arrow-right"
                icon-pos="right"
                :loading="saving"
                @click="next('3')"
              />
            </div>
          </div>
        </StepPanel>

        <!-- 4. Проживающие и особые условия -->
        <StepPanel value="4">
          <div class="form">
            <fieldset class="group">
              <legend>Проживающие</legend>
              <div v-for="(o, i) in form.occupants" :key="i" class="occupant">
                <div class="field grow">
                  <label :for="`w-occ-${i}`">ФИО *</label>
                  <InputText
                    :id="`w-occ-${i}`"
                    v-model="o.fullName"
                    :invalid="!!err('4', `occupants.${i}.fullName`)"
                    fluid
                    @blur="touch(`occupants.${i}.fullName`)"
                  />
                  <small v-if="err('4', `occupants.${i}.fullName`)" class="error">{{
                    err('4', `occupants.${i}.fullName`)
                  }}</small>
                </div>
                <div class="field">
                  <label :for="`w-occ-b-${i}`">Дата рождения</label>
                  <DateInput v-model="o.birthDate" :input-id="`w-occ-b-${i}`" :max-date="today()" />
                </div>
                <div class="field">
                  <label :for="`w-occ-r-${i}`">Кем приходится</label>
                  <InputText :id="`w-occ-r-${i}`" v-model="o.relation" placeholder="супруга, сын" fluid />
                </div>
                <Button
                  type="button"
                  icon="pi pi-trash"
                  text
                  severity="danger"
                  aria-label="Удалить проживающего"
                  class="occupant__del"
                  @click="form.occupants.splice(i, 1)"
                />
              </div>
              <Button type="button" label="Добавить" icon="pi pi-plus" text @click="addOccupant" />
            </fieldset>
            <div class="field">
              <label for="w-special">Особые условия</label>
              <Textarea id="w-special" v-model="form.specialTerms" rows="4" auto-resize fluid />
              <small class="hint">Попадают в текст договора.</small>
            </div>
            <div class="field">
              <label for="w-notes">Заметки</label>
              <Textarea id="w-notes" v-model="form.notes" rows="3" auto-resize fluid />
              <small class="hint">Внутренние, в документы не попадают.</small>
            </div>
            <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
            <div class="actions">
              <Button type="button" label="Назад" outlined @click="step = '3'" />
              <Button
                type="button"
                label="Далее"
                icon="pi pi-arrow-right"
                icon-pos="right"
                :loading="saving"
                @click="next('4')"
              />
            </div>
          </div>
        </StepPanel>

        <!-- 5. Проверка -->
        <StepPanel value="5">
          <div class="form">
            <div class="summary">
              <dl class="props">
                <dt>Вид</dt>
                <dd>
                  {{ basis ? legalBasisTitle[basis] : '—' }}<template v-if="shortTerm"> (краткосрочный)</template>
                </dd>
                <dt>Номер</dt>
                <dd>{{ saved ? `№ ${saved.number}` : '—' }}</dd>
                <dt>Объект</dt>
                <dd>{{ property?.label ?? '—' }}</dd>
                <dt>Наниматель</dt>
                <dd>{{ tenant?.label ?? '—' }}</dd>
                <dt>Срок</dt>
                <dd>{{ formatDate(form.startDate) }} – {{ formatDate(form.endDate) }}</dd>
                <dt>Плата в месяц</dt>
                <dd><MoneyText :kop="form.rentKop" />, день оплаты {{ form.paymentDay }}</dd>
                <dt>Срок первой оплаты</dt>
                <dd>{{ formatDate(form.firstDueDate) }}</dd>
                <dt>Залог</dt>
                <dd><MoneyText :kop="form.depositKop ?? 0" /></dd>
              </dl>
              <dl class="props">
                <dt>ЖКУ</dt>
                <dd>
                  {{ utilitiesModeLabels[form.utilitiesMode] }}
                  <template v-if="form.utilitiesMode !== 'included' && form.utilitiesFixedKop">
                    , фиксированная часть <MoneyText :kop="form.utilitiesFixedKop" />
                  </template>
                </dd>
                <dt>Пени</dt>
                <dd>
                  <template v-if="form.penaltyEnabled">
                    {{ form.penaltyRatePct }} % в день, льгота {{ form.penaltyGraceDays }} дн.<template
                      v-if="form.penaltyCapPct"
                    >
                      , не более {{ form.penaltyCapPct }} %</template
                    ><template v-if="form.penaltyOnUtilities">, в т.ч. на ЖКУ</template>
                  </template>
                  <template v-else>не начисляются</template>
                </dd>
                <dt>Индексация</dt>
                <dd>
                  <template v-if="form.indexationEnabled">
                    {{ form.indexationPct }} % раз в {{ form.indexationPeriodMonths }} мес., первая —
                    {{ indexationHint }}
                  </template>
                  <template v-else>нет</template>
                </dd>
                <dt>Проживающие</dt>
                <dd>{{ form.occupants.map((o) => o.fullName).join(', ') || '—' }}</dd>
                <dt>Особые условия</dt>
                <dd class="pre">{{ form.specialTerms || '—' }}</dd>
              </dl>
            </div>
            <Message v-for="w in saved?.warnings ?? warnings" :key="w" severity="warn" :closable="false">{{
              w
            }}</Message>
            <Message v-if="saved?.activationBlockers.length" severity="error" :closable="false">
              Ввести в действие пока нельзя: {{ saved.activationBlockers.join('; ') }}
            </Message>
            <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
            <div class="actions">
              <Button type="button" label="Назад" outlined @click="step = '4'" />
              <Button
                type="button"
                label="Сформировать проект договора (PDF)"
                icon="pi pi-file-pdf"
                outlined
                :loading="docs.contract.isPending.value"
                @click="projectPdf"
              />
              <Button type="button" label="Сохранить черновик" outlined :loading="saving" @click="saveDraft" />
              <Button
                type="button"
                label="Подписан — ввести в действие"
                icon="pi pi-check"
                :loading="saving"
                @click="openActivate"
              />
            </div>
          </div>
        </StepPanel>
      </StepPanels>
    </Stepper>

    <Dialog
      v-model:visible="tenantDialog"
      modal
      header="Новый наниматель"
      :style="{ width: '960px' }"
      :breakpoints="{ '1000px': '100vw' }"
    >
      <TenantFormPage embedded @saved="tenantCreated" @cancel="tenantDialog = false" />
    </Dialog>
    <ActivateDialog
      v-if="saved"
      v-model:visible="activateDialog"
      :contract="saved"
      @activated="(d) => router.push(`/contracts/${d.id}`)"
    />
  </div>
</template>

<style scoped>
:deep(.p-steplist) {
  flex-wrap: wrap;
  overflow: visible;
}
/* Белая подложка шага: у PrimeVue без внутренних отступов — поля упираются в край. Скругление — как у кнопок. */
:deep(.p-steppanel) {
  padding: 1.25rem 1.5rem;
  border-radius: var(--p-form-field-border-radius);
}
.row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: center;
}
.grow {
  flex: 1 1 260px;
  min-width: 0;
}
.group {
  border: 1px solid var(--p-content-border-color);
  border-radius: 6px;
  padding: 0.75rem 1rem 1rem;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.group legend {
  font-weight: 600;
  padding: 0 0.25rem;
}
.occupant {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 0.75rem;
}
.occupant__del {
  margin-bottom: 0.125rem;
}
.summary {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 1rem 2rem;
}
.props {
  display: grid;
  grid-template-columns: minmax(130px, 170px) 1fr;
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
</style>
