<script setup lang="ts">
// Взаиморасчёт при выезде (14 Экраны собственника, раздел 4.6; 05 разд. 4.9): долг, остаток залога, аванс по платежам.
// Кнопки активны, когда операция возможна; порядок не навязывается.
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { fetchActs, useContract, useContractSub } from '@/api/queries/contracts'
import { documentPdfUrl } from '@/api/queries/documents'
import { useSettlement } from '@/api/queries/payments'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import { formatDate } from '@/utils/date'
import { legalBasisLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import AdvanceDialog from '@/modules/payments/components/AdvanceDialog.vue'
import DepositOperationDialog from '@/modules/payments/components/DepositOperationDialog.vue'
import type { DepositOpKind } from '@/modules/payments/types'
import ReconciliationDialog from '../components/ReconciliationDialog.vue'

const route = useRoute()
const id = computed(() => route.params.id as string)
const contract = useContract(id)
const c = computed(() => contract.data.value)
const settlement = useSettlement(id)
const s = computed(() => settlement.data.value)
const acts = useContractSub(id, 'acts', fetchActs)
const returnAct = computed(() => (acts.data.value ?? []).find((a) => a.kind === 'return'))

const opKind = ref<DepositOpKind>('offset')
const opDialog = ref(false)
function openOp(kind: DepositOpKind) {
  opKind.value = kind
  opDialog.value = true
}

// «Вернуть переплату» — по платежу с авансом (05 разд. 10.6).
const refundPayment = ref<{ paymentId: string; advanceKop: number } | null>(null)
const refundDialog = computed({
  get: () => refundPayment.value !== null,
  set: (v: boolean) => {
    if (!v) refundPayment.value = null
  },
})
const advanceMenu = ref()
const advanceItems = computed(() =>
  (s.value?.paymentsWithAdvance ?? []).map((p) => ({
    label: `Платёж от ${formatDate(p.receivedOn)}`,
    command: () => (refundPayment.value = { paymentId: p.paymentId, advanceKop: p.advanceKop }),
  })),
)
function refundAdvance(e: Event) {
  const list = s.value?.paymentsWithAdvance ?? []
  if (list.length === 1) refundPayment.value = { paymentId: list[0]!.paymentId, advanceKop: list[0]!.advanceKop }
  else advanceMenu.value?.toggle(e)
}

const reconDialog = ref(false)
</script>

<template>
  <div>
    <Skeleton v-if="contract.isPending.value || settlement.isPending.value" height="12rem" />
    <Message v-else-if="settlement.isError.value" severity="error" :closable="false">
      {{ parseProblem(settlement.error.value).message }}
    </Message>
    <template v-else-if="c && s">
      <PageHeader
        title="Взаиморасчёт"
        :subtitle="`${legalBasisLabels[c.legalBasis]} № ${c.number}, ${c.property.code}, ${c.tenant.displayName}`"
      >
        <template #actions>
          <Button label="К договору" icon="pi pi-arrow-left" text @click="$router.push(`/contracts/${c.id}`)" />
        </template>
      </PageHeader>

      <div class="columns">
        <section class="block">
          <h3>Долг</h3>
          <p class="big"><MoneyText :kop="s.debtKop" /></p>
          <div v-for="ch in s.openCharges" :key="ch.id" class="row">
            <span>
              {{ ch.title }}
              <span class="hint">срок {{ formatDate(ch.dueDate) }}</span>
            </span>
            <MoneyText :kop="ch.outstandingKop" />
          </div>
          <p v-if="!s.openCharges.length" class="hint">Долга нет</p>
        </section>
        <section class="block">
          <h3>Залог</h3>
          <p class="big"><MoneyText :kop="s.depositBalanceKop" /></p>
          <p class="hint">Остаток обеспечительного платежа</p>
        </section>
        <section class="block">
          <h3>Аванс</h3>
          <p class="big"><MoneyText :kop="s.advanceKop" /></p>
          <div v-for="p in s.paymentsWithAdvance" :key="p.paymentId" class="row">
            <RouterLink :to="`/payments/${p.paymentId}`">Платёж от {{ formatDate(p.receivedOn) }}</RouterLink>
            <MoneyText :kop="p.advanceKop" />
          </div>
        </section>
      </div>

      <div class="actions actions--start toolbar">
        <Button
          label="Зачесть залог в счёт долга"
          icon="pi pi-arrow-right-arrow-left"
          :disabled="s.depositBalanceKop <= 0 || s.debtKop <= 0"
          @click="openOp('offset')"
        />
        <Button
          label="Удержать из залога"
          icon="pi pi-minus-circle"
          outlined
          :disabled="s.depositBalanceKop <= 0"
          @click="openOp('withhold')"
        />
        <Button
          label="Вернуть залог"
          icon="pi pi-replay"
          outlined
          :disabled="s.depositBalanceKop <= 0"
          @click="openOp('refund')"
        />
        <Button
          label="Вернуть переплату"
          icon="pi pi-wallet"
          outlined
          :disabled="s.advanceKop <= 0"
          @click="refundAdvance"
        />
        <Menu ref="advanceMenu" :model="advanceItems" popup />
      </div>

      <section class="block">
        <h3>Акт возврата</h3>
        <template v-if="returnAct">
          <p>
            {{ returnAct.signedOn ? `Подписан ${formatDate(returnAct.signedOn)}` : 'Черновик' }}
            <a v-if="returnAct.documentId" :href="documentPdfUrl(returnAct.documentId)" target="_blank" rel="noopener"
              >Открыть PDF</a
            >
          </p>
        </template>
        <p v-else class="hint">Акт возврата не создан.</p>
        <div class="actions actions--start">
          <Button label="Акты договора" text @click="$router.push(`/contracts/${c.id}?tab=acts`)" />
          <Button label="Сформировать акт сверки" icon="pi pi-list" outlined @click="reconDialog = true" />
        </div>
      </section>

      <DepositOperationDialog
        v-model:visible="opDialog"
        :contract-id="c.id"
        :balance-kop="s.depositBalanceKop"
        :kind="opKind"
      />
      <AdvanceDialog
        v-if="refundPayment"
        v-model:visible="refundDialog"
        :payment-id="refundPayment.paymentId"
        :advance-kop="refundPayment.advanceKop"
        mode="refund"
      />
      <ReconciliationDialog v-model:visible="reconDialog" :contract="c" />
    </template>
  </div>
</template>

<style scoped>
.columns {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 1rem;
}
.block {
  background: var(--p-content-background);
  border: 1px solid var(--p-content-border-color);
  border-radius: 6px;
  padding: 0.75rem 1rem;
  margin-bottom: 1rem;
}
.block h3 {
  margin: 0 0 0.5rem;
}
.big {
  font-size: 1.25rem;
  font-weight: 600;
  margin: 0 0 0.5rem;
}
.row {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.125rem 0;
}
.toolbar {
  margin-bottom: 1rem;
}
.actions--start {
  justify-content: flex-start;
}
</style>
