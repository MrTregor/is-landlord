<script setup lang="ts">
// Результат оплаты (15 Экраны нанимателя, раздел 3): опрос статуса каждые 2 с до 30 с.
import { computed, onBeforeUnmount, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { cabinetReceiptUrl, useCabinetPaymentStatus, useStartPayment } from '@/api/queries/cabinet'
import MoneyText from '@/components/MoneyText.vue'
import { parseProblem } from '@/utils/problem'

const POLL_MS = 30_000

const route = useRoute()
const router = useRouter()
const id = computed(() => route.params.id as string)
const timedOut = ref(false)
const timer = setTimeout(() => (timedOut.value = true), POLL_MS)
onBeforeUnmount(() => clearTimeout(timer))

const status = useCabinetPaymentStatus(id, timedOut)
const p = computed(() => status.data.value)

// «Попробовать снова» — новая оплата тем же составом (15 разд. 3).
const start = useStartPayment()
const retryError = ref<string | null>(null)
async function retry() {
  retryError.value = null
  try {
    const r = await start.mutateAsync({
      contractId: p.value!.contractId,
      purpose: p.value!.purpose,
      chargeIds: p.value!.chargeIds,
    })
    window.location.assign(r.paymentUrl)
  } catch (err) {
    retryError.value = parseProblem(err).message
  }
}
</script>

<template>
  <div class="result">
    <Message v-if="status.isError.value" severity="error" :closable="false">
      {{ parseProblem(status.error.value).message }}
    </Message>
    <template v-else-if="!p || (p.status === 'pending' && !timedOut)">
      <ProgressSpinner style="width: 48px; height: 48px" aria-label="Проверяем оплату" />
      <h1>Проверяем оплату…</h1>
    </template>
    <template v-else-if="p.status === 'succeeded'">
      <i class="pi pi-check-circle icon icon--ok" aria-hidden="true" />
      <h1>Оплата прошла</h1>
      <div class="big"><MoneyText :kop="p.amountKop" /></div>
      <a v-if="p.receiptNumber" :href="cabinetReceiptUrl(p.id)" target="_blank" rel="noopener">
        Квитанция {{ p.receiptNumber }}
      </a>
      <p class="hint">Если по объекту формируется кассовый чек, он придёт на e-mail.</p>
      <Button label="На главную" class="wide" @click="router.push('/cabinet')" />
    </template>
    <template v-else-if="p.status === 'pending'">
      <i class="pi pi-clock icon" aria-hidden="true" />
      <h1>Ожидаем подтверждение банка</h1>
      <p>Статус обновится автоматически, квитанция придёт на e-mail.</p>
      <Button label="На главную" class="wide" @click="router.push('/cabinet')" />
    </template>
    <template v-else>
      <i class="pi pi-times-circle icon icon--fail" aria-hidden="true" />
      <h1>Оплата не прошла</h1>
      <Message v-if="retryError" severity="error" :closable="false">{{ retryError }}</Message>
      <Button label="Попробовать снова" class="wide" @click="retry" />
      <Button label="На главную" outlined class="wide" @click="router.push('/cabinet')" />
    </template>
  </div>
</template>

<style scoped>
.result {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 0.75rem;
  padding-top: 2rem;
}
.result h1 {
  font-size: 1.25rem;
  margin: 0;
}
.icon {
  font-size: 3rem;
  color: var(--p-text-muted-color);
}
.icon--ok {
  color: var(--p-green-600);
}
.icon--fail {
  color: var(--p-red-600);
}
.big {
  font-size: 1.75rem;
  font-weight: 700;
}
.wide {
  width: 100%;
  max-width: 360px;
  min-height: 44px;
}
</style>
