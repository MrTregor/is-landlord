<script setup lang="ts">
// Начисления и платежи кабинета (15 Экраны нанимателя, раздел 4): на телефоне — карточки, ≥ 768 px — те же
// карточки в две колонки. Договор — из адреса (?contract=) или действующий.
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  cabinetReceiptUrl,
  useCabinetCharges,
  useCabinetPayments,
  useCabinetSummary,
  type CabinetCharge,
} from '@/api/queries/cabinet'
import MoneyText from '@/components/MoneyText.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import {
  chargeStatusLabels,
  chargeStatusSeverity,
  paymentMethodLabels,
  paymentPurposeLabels,
  paymentStatusLabels,
  paymentStatusSeverity,
} from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { utilityLines } from '@/modules/charges/calcDetails'

const route = useRoute()
const router = useRouter()
const queryContract = computed(() => (typeof route.query.contract === 'string' ? route.query.contract : undefined))
const summary = useCabinetSummary(queryContract)
const contractId = computed(() => summary.data.value?.contractId ?? undefined)
const charges = useCabinetCharges(contractId)
const payments = useCabinetPayments(contractId)

const tab = computed({
  get: () => (route.query.tab === 'payments' ? 'payments' : 'charges'),
  set: (v: string | number) =>
    void router.replace({ query: { ...route.query, tab: v === 'payments' ? 'payments' : undefined } }),
})
const unpaidOnly = ref<'all' | 'unpaid'>('all')
const list = computed(() =>
  (charges.data.value ?? []).filter((c) => unpaidOnly.value === 'all' || c.outstandingKop > 0),
)
const opened = ref<string | null>(null)

/** Строки счётчиков ЖКУ (расход, тариф, сумма). */
function meterLines(c: CabinetCharge) {
  return c.kind === 'utilities' && c.calcDetails ? utilityLines(c.calcDetails) : []
}
</script>

<template>
  <div>
    <h1 class="title">Начисления и платежи</h1>
    <Message v-if="summary.isError.value" severity="error" :closable="false">
      {{ parseProblem(summary.error.value).message }}
    </Message>
    <Message v-else-if="summary.data.value && !contractId" severity="info" :closable="false"
      >Договоров пока нет.</Message
    >
    <Tabs v-else v-model:value="tab">
      <TabList>
        <Tab value="charges">Начисления</Tab>
        <Tab value="payments">Платежи</Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="charges">
          <SelectButton
            v-model="unpaidOnly"
            :options="[
              { value: 'all', label: 'Все' },
              { value: 'unpaid', label: 'Неоплаченные' },
            ]"
            option-label="label"
            option-value="value"
            :allow-empty="false"
            aria-label="Фильтр начислений"
            class="filter"
          />
          <Skeleton v-if="charges.isPending.value" height="6rem" />
          <p v-else-if="!list.length" class="hint">Начислений нет</p>
          <div class="cards">
            <article v-for="c in list" :key="c.id" class="card">
              <div class="card__head">
                <strong>{{ c.title }}</strong>
                <MoneyText :kop="c.amountKop" />
              </div>
              <div class="hint">Срок оплаты: {{ formatDate(c.dueDate) || '—' }}</div>
              <div class="row">
                <span>Оплачено <MoneyText :kop="c.paidKop" /></span>
                <span>Остаток <MoneyText :kop="c.outstandingKop" /></span>
              </div>
              <div class="tags">
                <StatusTag :label="chargeStatusLabels[c.status]" :severity="chargeStatusSeverity[c.status]" />
                <Tag v-if="c.isOverdue" :value="`Просрочено ${c.overdueDays} дн.`" severity="danger" />
              </div>
              <p v-if="c.penaltyText" class="hint">{{ c.penaltyText }}</p>
              <template v-if="meterLines(c).length">
                <Button
                  :label="opened === c.id ? 'Скрыть расчёт' : 'Показать расчёт'"
                  link
                  size="small"
                  @click="opened = opened === c.id ? null : c.id"
                />
                <ul v-if="opened === c.id" class="util">
                  <li v-for="(l, i) in meterLines(c)" :key="i">
                    {{ l.type }}: {{ l.consumption }} × {{ l.tariff }} = {{ l.amount }}
                    <span v-if="l.missing" class="hint">(показание не передано)</span>
                  </li>
                </ul>
              </template>
            </article>
          </div>
        </TabPanel>
        <TabPanel value="payments">
          <Skeleton v-if="payments.isPending.value" height="6rem" />
          <p v-else-if="!payments.data.value?.length" class="hint">Платежей нет</p>
          <div class="cards">
            <article v-for="p in payments.data.value ?? []" :key="p.id" class="card">
              <div class="card__head">
                <strong>{{ formatDate(p.receivedOn ?? p.createdAt.slice(0, 10)) }}</strong>
                <MoneyText :kop="p.amountKop" />
              </div>
              <div class="hint">{{ paymentPurposeLabels[p.purpose] }} · {{ paymentMethodLabels[p.method] }}</div>
              <div class="tags">
                <StatusTag :label="paymentStatusLabels[p.status]" :severity="paymentStatusSeverity[p.status]" />
              </div>
              <div class="links">
                <a v-if="p.receiptUrl" :href="cabinetReceiptUrl(p.id)" target="_blank" rel="noopener">
                  Квитанция {{ p.receiptNumber }}
                </a>
                <a
                  v-if="p.ofdReceiptUrl ?? p.npdReceiptUrl"
                  :href="(p.ofdReceiptUrl ?? p.npdReceiptUrl)!"
                  target="_blank"
                  rel="noopener"
                >
                  Чек
                </a>
              </div>
            </article>
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </div>
</template>

<style scoped>
.title {
  font-size: 1.25rem;
  margin: 0 0 0.75rem;
}
.filter {
  margin: 0.75rem 0;
}
.cards {
  display: grid;
  gap: 0.75rem;
  margin-top: 0.75rem;
}
@media (min-width: 768px) {
  .cards {
    grid-template-columns: 1fr 1fr;
  }
}
.card {
  background: var(--p-content-background);
  border: 1px solid var(--p-content-border-color);
  border-radius: 8px;
  padding: 0.75rem 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}
.card__head,
.row {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
}
.tags,
.links {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 1rem;
}
.links a {
  min-height: 44px;
  display: inline-flex;
  align-items: center;
}
.util {
  margin: 0;
  padding-left: 1rem;
}
</style>
