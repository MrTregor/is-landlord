<script setup lang="ts">
// Главная кабинета (15 Экраны нанимателя, раздел 2): «К оплате» с онлайн-оплатой и выбором начислений,
// обеспечительный платёж, аванс. Карточки показаний и заявок появятся на этапах M11 и M13.
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useCabinetSummary, useStartPayment } from '@/api/queries/cabinet'
import MoneyText from '@/components/MoneyText.vue'
import { formatDate } from '@/utils/date'
import { formatKop } from '@/utils/money'
import { parseProblem } from '@/utils/problem'

const route = useRoute()
const router = useRouter()
const contractId = computed(() => (typeof route.query.contract === 'string' ? route.query.contract : undefined))
const summary = useCabinetSummary(contractId)
const s = computed(() => summary.data.value)
const current = computed(() => s.value?.contracts.find((c) => c.id === s.value?.contractId))
const contractOptions = computed(() =>
  (s.value?.contracts ?? []).map((c) => ({ value: c.id, label: `№ ${c.number}, ${c.propertyAddress}` })),
)
function selectContract(id: string) {
  void router.replace({ query: { contract: id } })
}

const start = useStartPayment()
const payError = ref<string | null>(null)

/** Начать оплату и перейти на страницу банка. */
async function pay(purpose: 'charges' | 'deposit', chargeIds: string[] = []) {
  payError.value = null
  try {
    const r = await start.mutateAsync({ contractId: s.value!.contractId!, purpose, chargeIds })
    window.location.assign(r.paymentUrl)
  } catch (err) {
    payError.value = parseProblem(err).message
  }
}

// «Выбрать, что оплатить».
const choose = ref(false)
const chosen = ref<string[]>([])
watch(choose, (v) => {
  if (v) chosen.value = (s.value?.lines ?? []).map((l) => l.chargeId)
})
const chosenTotal = computed(() =>
  (s.value?.lines ?? []).filter((l) => chosen.value.includes(l.chargeId)).reduce((a, l) => a + l.outstandingKop, 0),
)
</script>

<template>
  <div class="home">
    <Skeleton v-if="summary.isPending.value" height="10rem" />
    <Message v-else-if="summary.isError.value" severity="error" :closable="false">
      {{ parseProblem(summary.error.value).message }}
    </Message>
    <Message v-else-if="s && !s.contractId" severity="info" :closable="false">
      Договоров пока нет. Когда договор будет введён в действие, здесь появятся начисления и оплата.
    </Message>
    <template v-else-if="s && current">
      <header class="contract">
        <Select
          v-if="s.contracts.length > 1"
          :model-value="s.contractId"
          :options="contractOptions"
          option-label="label"
          option-value="value"
          aria-label="Договор"
          fluid
          @update:model-value="selectContract"
        />
        <template v-else>
          <div class="contract__number">Договор № {{ current.number }}</div>
          <div class="hint">{{ current.propertyAddress }}</div>
        </template>
      </header>

      <section class="card">
        <h2>К оплате</h2>
        <div class="big"><MoneyText :kop="s.dueTotalKop" /></div>
        <Tag v-if="s.overdue" severity="danger" :value="`Просрочено с ${formatDate(s.nextDueDate)}`" />
        <Tag
          v-else-if="s.dueTotalKop > 0 && s.nextDueDate"
          severity="info"
          :value="`Оплатить до ${formatDate(s.nextDueDate)}`"
        />
        <Tag v-else severity="success" value="Задолженности нет" />
        <ul v-if="s.lines.length" class="lines">
          <li v-for="l in s.lines" :key="l.chargeId" :class="{ overdue: l.isOverdue }">
            <span>
              {{ l.title }}
              <span class="hint">до {{ formatDate(l.dueDate) }}</span>
            </span>
            <MoneyText :kop="l.outstandingKop" />
          </li>
        </ul>
        <p v-if="s.penaltiesAccruingKop > 0" class="hint">
          Пени начисляются: <MoneyText :kop="s.penaltiesAccruingKop" /> — будут выставлены после погашения долга.
        </p>
        <Message v-if="s.pendingPayment" severity="warn" :closable="false" class="pending">
          У вас есть начатая оплата на {{ formatKop(s.pendingPayment.amountKop) }}.
          <a :href="s.pendingPayment.paymentUrl">Продолжить оплату</a>
        </Message>
        <template v-if="s.dueTotalKop > 0">
          <Button
            label="Оплатить картой или СБП"
            icon="pi pi-credit-card"
            class="wide"
            :loading="start.isPending.value"
            @click="pay('charges')"
          />
          <Button v-if="s.lines.length > 1" label="Выбрать, что оплатить" link @click="choose = true" />
        </template>
        <Message v-if="payError" severity="error" :closable="false">{{ payError }}</Message>
      </section>

      <section v-if="s.depositExpectedKop > 0" class="card">
        <h2>Обеспечительный платёж</h2>
        <p>
          Внесено <MoneyText :kop="s.depositExpectedKop - s.depositToPayKop" /> из
          <MoneyText :kop="s.depositExpectedKop" />, остаток <MoneyText :kop="s.depositBalanceKop" />
        </p>
        <Button
          v-if="s.depositToPayKop > 0"
          :label="`Внести ${formatKop(s.depositToPayKop)}`"
          icon="pi pi-lock"
          outlined
          class="wide"
          :loading="start.isPending.value"
          @click="pay('deposit')"
        />
      </section>

      <section v-if="s.advanceKop > 0" class="card">
        <h2>Аванс</h2>
        <p>Переплата <MoneyText :kop="s.advanceKop" /> будет зачтена в следующие начисления.</p>
      </section>

      <RouterLink to="/cabinet/charges" class="more">Начисления и платежи</RouterLink>
      <RouterLink to="/cabinet/contract" class="more">Договор и документы</RouterLink>

      <Dialog
        v-model:visible="choose"
        modal
        header="Что оплатить"
        :style="{ width: '480px' }"
        :breakpoints="{ '520px': '100vw' }"
      >
        <div class="choose">
          <div v-for="l in s.lines" :key="l.chargeId" class="field--check choose__row">
            <Checkbox v-model="chosen" :input-id="`ch-${l.chargeId}`" :value="l.chargeId" />
            <label :for="`ch-${l.chargeId}`" class="choose__label">
              <span>{{ l.title }}</span>
              <MoneyText :kop="l.outstandingKop" />
            </label>
          </div>
        </div>
        <div class="actions">
          <Button
            :label="`Оплатить ${formatKop(chosenTotal)}`"
            class="wide"
            :disabled="!chosen.length"
            :loading="start.isPending.value"
            @click="pay('charges', chosen)"
          />
        </div>
      </Dialog>
    </template>
  </div>
</template>

<style scoped>
.home {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.contract__number {
  font-weight: 600;
  font-size: 1.1rem;
}
.card {
  background: var(--p-content-background);
  border: 1px solid var(--p-content-border-color);
  border-radius: 8px;
  padding: 1rem;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.5rem;
}
.card h2 {
  margin: 0;
  font-size: 1rem;
  color: var(--p-text-muted-color);
}
.card p {
  margin: 0;
}
.big {
  font-size: 2rem;
  font-weight: 700;
}
.lines {
  list-style: none;
  padding: 0;
  margin: 0;
  width: 100%;
}
.lines li {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.375rem 0;
  border-bottom: 1px solid var(--p-content-border-color);
}
.lines li.overdue {
  color: var(--p-red-600);
}
.wide {
  width: 100%;
  min-height: 44px;
}
.pending {
  width: 100%;
}
.more {
  display: block;
  padding: 0.75rem 0;
  min-height: 44px;
}
.choose {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  margin-bottom: 1rem;
}
.choose__row {
  min-height: 44px;
}
.choose__label {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  flex: 1;
}
</style>
