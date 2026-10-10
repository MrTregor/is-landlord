<script setup lang="ts">
// Боковая панель начисления (14 Экраны собственника, раздел 5): поля, расчёт, распределения, корректировки, действия.
import { computed, reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useCharge, useChargeMutations } from '@/api/queries/charges'
import MoneyInput from '@/components/MoneyInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import {
  adjustmentKindLabels,
  chargeKindLabel,
  chargeOriginLabels,
  chargeStatusLabels,
  chargeStatusSeverity,
  penaltyStateLabels,
} from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { penaltySegments, rentFormula, utilityLines } from '../calcDetails'

const chargeId = defineModel<string | null>('chargeId', { required: true })
const toast = useToast()
const charge = useCharge(chargeId)
const m = useChargeMutations()
const c = computed(() => charge.data.value)
const visible = computed({
  get: () => !!chargeId.value,
  set: (v: boolean) => {
    if (!v) chargeId.value = null
  },
})
const active = computed(() => !!c.value && c.value.status !== 'cancelled')
const isPenalty = computed(() => c.value?.kind === 'penalty')
const details = computed(() => (c.value?.calcDetails ?? {}) as Record<string, unknown>)
const writtenOff = computed(() => isPenalty.value && c.value?.penaltyState === 'manual' && c.value.amountKop === 0)

// Диалог действия: корректировка, отмена, сумма пеней, списание.
type Action = 'adjust' | 'cancel' | 'penalty' | 'writeoff'
const action = ref<Action | null>(null)
const form = reactive({ amount: null as number | null, reason: '' })
const submitted = ref(false)
const error = ref<string | null>(null)
const titles: Record<Action, string> = {
  adjust: 'Корректировка',
  cancel: 'Отменить начисление',
  penalty: 'Изменить сумму пеней',
  writeoff: 'Списать пени',
}
function open(a: Action) {
  action.value = a
  form.amount = a === 'penalty' ? (c.value?.amountKop ?? 0) : null
  form.reason = ''
  submitted.value = false
  error.value = null
}
watch(chargeId, () => (action.value = null))
const errors = computed(() => {
  const e: Record<string, string> = {}
  if (form.reason.trim().length < 5) e.reason = 'Причина — не короче 5 символов'
  if (action.value === 'adjust' && !form.amount) e.amount = 'Укажите сумму изменения (со знаком минус — уменьшение)'
  if (action.value === 'penalty' && (form.amount == null || form.amount < 0)) e.amount = 'Укажите сумму пеней'
  return e
})
async function submit() {
  submitted.value = true
  if (Object.keys(errors.value).length || !c.value || !action.value) return
  const id = c.value.id
  const reason = form.reason.trim()
  try {
    if (action.value === 'adjust') await m.adjust.mutateAsync({ id, body: { deltaKop: form.amount!, reason } })
    if (action.value === 'cancel') await m.cancel.mutateAsync({ id, reason })
    if (action.value === 'penalty') await m.setPenalty.mutateAsync({ id, body: { amountKop: form.amount!, reason } })
    if (action.value === 'writeoff') await m.writeOff.mutateAsync({ id, reason })
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
    action.value = null
  } catch (err) {
    error.value = parseProblem(err).message
  }
}
const pending = computed(
  () =>
    m.adjust.isPending.value || m.cancel.isPending.value || m.setPenalty.isPending.value || m.writeOff.isPending.value,
)
</script>

<template>
  <Drawer v-model:visible="visible" position="right" :style="{ width: '520px', maxWidth: '100vw' }" header="Начисление">
    <Skeleton v-if="charge.isPending.value" height="10rem" />
    <template v-else-if="c">
      <h3 class="title">{{ c.title }}</h3>
      <div class="tags">
        <StatusTag :label="chargeStatusLabels[c.status]" :severity="chargeStatusSeverity[c.status]" />
        <Tag v-if="c.isOverdue" :value="`Просрочено ${c.overdueDays} дн.`" severity="danger" />
        <Tag v-if="writtenOff" value="Списаны" severity="secondary" />
        <Tag v-else-if="c.penaltyState" :value="penaltyStateLabels[c.penaltyState]" severity="info" />
        <Tag :value="chargeOriginLabels[c.origin]" severity="secondary" />
      </div>
      <dl class="props">
        <dt>Вид</dt>
        <dd>{{ chargeKindLabel(c.kind) }}</dd>
        <dt>Договор</dt>
        <dd>
          <RouterLink :to="`/contracts/${c.contract.id}?tab=charges`">№ {{ c.contract.number }}</RouterLink>
        </dd>
        <dt>Объект</dt>
        <dd>{{ c.property.code }}</dd>
        <dt>Наниматель</dt>
        <dd>{{ c.tenant.displayName }}</dd>
        <template v-if="c.periodStart">
          <dt>Период</dt>
          <dd>{{ formatDate(c.periodStart) }} – {{ formatDate(c.periodEnd) }}</dd>
        </template>
        <dt>Срок оплаты</dt>
        <dd>{{ formatDate(c.dueDate) || 'нет (пени начисляются)' }}</dd>
        <dt>Сумма</dt>
        <dd>
          <MoneyText :kop="c.amountKop" />
          <span v-if="c.originalAmountKop !== c.amountKop" class="hint">
            (было <MoneyText :kop="c.originalAmountKop" />)</span
          >
        </dd>
        <dt>Оплачено</dt>
        <dd><MoneyText :kop="c.paidKop" /></dd>
        <dt>Остаток</dt>
        <dd><MoneyText :kop="c.outstandingKop" /></dd>
        <template v-if="c.cancelReason">
          <dt>Причина отмены</dt>
          <dd>{{ c.cancelReason }}</dd>
        </template>
      </dl>

      <section class="block">
        <h4>Расчёт</h4>
        <p v-if="rentFormula(details)">{{ rentFormula(details) }}</p>
        <DataTable v-if="utilityLines(details).length" :value="utilityLines(details)" size="small">
          <Column field="type" header="Счётчик" />
          <Column header="Предыдущее">
            <template #body="{ data }"
              >{{ data.prev }} <span class="hint">({{ data.prevDate }})</span></template
            >
          </Column>
          <Column header="Текущее">
            <template #body="{ data }">
              {{ data.value }}
              <Tag v-if="data.missing" value="нет показания" severity="warn" />
            </template>
          </Column>
          <Column field="consumption" header="Расход" />
          <Column field="tariff" header="Тариф" />
          <Column field="amount" header="Сумма" />
        </DataTable>
        <p v-if="typeof details.fixedKop === 'number' && details.fixedKop > 0">
          Фиксированная часть: <MoneyText :kop="details.fixedKop as number" />
        </p>
        <ul v-if="penaltySegments(details).length" class="segments">
          <li v-for="s in penaltySegments(details)" :key="s" class="tabular-nums">{{ s }}</li>
        </ul>
        <p v-if="typeof details.capKop === 'number'" class="hint">
          Предел пеней: <MoneyText :kop="details.capKop as number" />
        </p>
        <p v-if="details.reason" class="hint">Основание: {{ details.reason }}</p>
      </section>

      <section class="block">
        <h4>Распределения платежей</h4>
        <p v-if="!c.allocations.length" class="hint">Оплат нет</p>
        <div v-for="a in c.allocations" :key="a.paymentId + a.amountKop" class="row">
          <span>{{ a.receiptNumber ?? 'платёж' }} от {{ formatDate(a.receivedOn) }}</span>
          <MoneyText :kop="a.amountKop" />
        </div>
      </section>

      <section v-if="c.adjustments.length" class="block">
        <h4>Корректировки</h4>
        <div v-for="a in c.adjustments" :key="a.id" class="row">
          <span>
            {{ adjustmentKindLabels[a.kind] }} · {{ a.reason }}
            <span class="hint"
              >· {{ formatDateTime(a.createdAt) }}<template v-if="a.createdBy">, {{ a.createdBy }}</template></span
            >
          </span>
          <MoneyText :kop="a.deltaKop" />
        </div>
      </section>

      <section v-if="c.penalty" class="block">
        <h4>Пени по этому начислению</h4>
        <div class="row">
          <a href="#" @click.prevent="chargeId = c.penalty.id">{{ c.penalty.title }}</a>
          <MoneyText :kop="c.penalty.amountKop" />
        </div>
      </section>

      <div v-if="active" class="actions actions--start">
        <template v-if="!isPenalty">
          <Button label="Корректировка" icon="pi pi-pencil" outlined @click="open('adjust')" />
          <Button label="Отменить начисление" icon="pi pi-times" severity="danger" outlined @click="open('cancel')" />
        </template>
        <template v-else>
          <Button label="Изменить сумму пеней" icon="pi pi-pencil" outlined @click="open('penalty')" />
          <Button label="Списать пени" icon="pi pi-eraser" severity="danger" outlined @click="open('writeoff')" />
        </template>
      </div>
    </template>

    <Dialog
      :visible="!!action"
      modal
      :header="action ? titles[action] : ''"
      :style="{ width: '440px' }"
      :breakpoints="{ '480px': '100vw' }"
      @update:visible="(v: boolean) => !v && (action = null)"
    >
      <form class="form" novalidate @submit.prevent="submit">
        <div v-if="action === 'adjust' || action === 'penalty'" class="field">
          <label for="ch-amount">{{ action === 'adjust' ? 'Сумма изменения *' : 'Сумма пеней *' }}</label>
          <MoneyInput
            v-model="form.amount"
            input-id="ch-amount"
            :allow-negative="action === 'adjust'"
            :invalid="submitted && !!errors.amount"
          />
          <small v-if="submitted && errors.amount" class="error">{{ errors.amount }}</small>
          <small v-else-if="action === 'adjust'" class="hint">Со знаком минус — уменьшение</small>
        </div>
        <p v-if="action === 'cancel'" class="hint">Оплаты по начислению станут авансом.</p>
        <div class="field">
          <label for="ch-reason">Причина *</label>
          <Textarea
            id="ch-reason"
            v-model="form.reason"
            rows="2"
            auto-resize
            :invalid="submitted && !!errors.reason"
            fluid
          />
          <small v-if="submitted && errors.reason" class="error">{{ errors.reason }}</small>
        </div>
        <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="action = null" />
          <Button
            type="submit"
            label="Сохранить"
            :severity="action === 'cancel' || action === 'writeoff' ? 'danger' : undefined"
            :loading="pending"
          />
        </div>
      </form>
    </Dialog>
  </Drawer>
</template>

<style scoped>
.title {
  margin: 0 0 0.5rem;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
  margin-bottom: 1rem;
}
.props {
  display: grid;
  grid-template-columns: 130px 1fr;
  gap: 0.375rem 1rem;
  margin: 0;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
}
.block {
  margin-top: 1.25rem;
}
.block h4 {
  margin: 0 0 0.5rem;
}
.row {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.25rem 0;
  border-bottom: 1px solid var(--p-content-border-color);
}
.segments {
  margin: 0;
  padding-left: 1.25rem;
}
.actions--start {
  justify-content: flex-start;
  margin-top: 1.5rem;
}
</style>
