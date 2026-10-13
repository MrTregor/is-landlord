<script setup lang="ts">
// Карточка платежа (14 Экраны собственника, раздел 6.3): поля, распределение, аванс, возвраты, чек, квитанция,
// отмена платежа, статус онлайн-оплаты в банке и «Проверить статус». Чек — этап M8, строка выписки — этап M9.
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { receiptPdfUrl, usePayment, usePaymentMutations, type PaymentDetail } from '@/api/queries/payments'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import {
  chargeKindLabel,
  fiscalStatusLabels,
  fiscalStatusSeverity,
  paymentMethodLabels,
  paymentPurposeLabels,
  paymentSourceLabels,
  paymentStatusLabels,
  paymentStatusSeverity,
  refundMethodLabels,
} from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import AdvanceDialog from '../components/AdvanceDialog.vue'

const route = useRoute()
const toast = useToast()
const id = computed(() => route.params.id as string)
const payment = usePayment(id)
const p = computed(() => payment.data.value)
const m = usePaymentMutations()

const cancellable = computed(
  () =>
    p.value?.status === 'succeeded' &&
    p.value.source !== 'online' &&
    !['done', 'queued', 'wait'].includes(p.value.fiscalStatus ?? ''),
)
const activeAllocations = computed(() => (p.value?.allocations ?? []).filter((a) => !a.reversedAt))
const reversedAllocations = computed(() => (p.value?.allocations ?? []).filter((a) => !!a.reversedAt))

const advanceMode = ref<'refund' | 'deposit' | null>(null)
const advanceDialog = computed({
  get: () => advanceMode.value !== null,
  set: (v: boolean) => {
    if (!v) advanceMode.value = null
  },
})

async function checkStatus() {
  try {
    const r = await m.checkStatus.mutateAsync(id.value)
    toast.add({ severity: 'info', summary: `Статус платежа: ${paymentStatusLabels[r.status]}`, life: 3000 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
  }
}

const cancelDialog = ref(false)
const cancelReason = ref('')
const cancelError = ref<string | null>(null)
function openCancel() {
  cancelReason.value = ''
  cancelError.value = null
  cancelDialog.value = true
}
async function cancelPayment() {
  if (!cancelReason.value.trim()) {
    cancelError.value = 'Укажите причину'
    return
  }
  try {
    await m.cancel.mutateAsync({ id: id.value, reason: cancelReason.value.trim() })
    cancelDialog.value = false
    toast.add({ severity: 'success', summary: 'Платёж отменён', life: 3000 })
  } catch (err) {
    cancelError.value = parseProblem(err).message
  }
}
</script>

<template>
  <div>
    <Skeleton v-if="payment.isPending.value" height="12rem" />
    <Message v-else-if="payment.isError.value" severity="error" :closable="false">
      {{ parseProblem(payment.error.value).message }}
    </Message>
    <template v-else-if="p">
      <PageHeader :title="p.receiptNumber ? `Платёж ${p.receiptNumber}` : 'Платёж'">
        <template #actions>
          <Button
            v-if="p.status === 'succeeded'"
            as="a"
            :href="receiptPdfUrl(p.id)"
            target="_blank"
            rel="noopener"
            label="Квитанция"
            icon="pi pi-file-pdf"
            outlined
          />
          <Button
            v-if="cancellable"
            label="Отменить платёж"
            icon="pi pi-times"
            severity="danger"
            outlined
            @click="openCancel"
          />
        </template>
      </PageHeader>

      <div class="grid">
        <section class="block">
          <h3>Платёж</h3>
          <dl class="props">
            <dt>Статус</dt>
            <dd><StatusTag :label="paymentStatusLabels[p.status]" :severity="paymentStatusSeverity[p.status]" /></dd>
            <dt>Договор</dt>
            <dd>
              <RouterLink :to="`/contracts/${p.contract.id}`">№ {{ p.contract.number }}</RouterLink
              >,
              {{ p.property.code }}
            </dd>
            <dt>Наниматель</dt>
            <dd>
              <RouterLink :to="`/tenants/${p.tenant.id}`">{{ p.tenant.displayName }}</RouterLink>
            </dd>
            <dt>Назначение</dt>
            <dd>{{ paymentPurposeLabels[p.purpose] }}</dd>
            <dt>Сумма</dt>
            <dd><MoneyText :kop="p.amountKop" /></dd>
            <dt>Дата поступления</dt>
            <dd>{{ formatDate(p.receivedOn) || '—' }}</dd>
            <dt>Способ</dt>
            <dd>{{ paymentMethodLabels[p.method] }}</dd>
            <dt>Источник</dt>
            <dd>{{ paymentSourceLabels[p.source] }}</dd>
            <template v-if="p.payerName">
              <dt>Плательщик</dt>
              <dd>{{ p.payerName }}</dd>
            </template>
            <template v-if="p.comment">
              <dt>Комментарий</dt>
              <dd>{{ p.comment }}</dd>
            </template>
            <template v-if="p.source === 'online'">
              <dt>Статус в банке</dt>
              <dd>
                {{ p.providerStatus ?? '—' }}
                <Button
                  v-if="p.status === 'pending'"
                  label="Проверить статус"
                  icon="pi pi-refresh"
                  size="small"
                  text
                  :loading="m.checkStatus.isPending.value"
                  @click="checkStatus"
                />
              </dd>
            </template>
            <template v-if="p.status === 'cancelled'">
              <dt>Отменён</dt>
              <dd>{{ p.cancelReason }}</dd>
            </template>
          </dl>
        </section>

        <section v-if="p.purpose === 'charges'" class="block">
          <h3>Аванс</h3>
          <p class="big"><MoneyText :kop="p.advanceKop" /></p>
          <p class="hint">Нераспределённый остаток погашает новые начисления договора.</p>
          <div v-if="p.status === 'succeeded' && p.advanceKop > 0" class="actions actions--start">
            <Button label="Вернуть" icon="pi pi-replay" outlined @click="advanceMode = 'refund'" />
            <Button label="Перенести в залог" icon="pi pi-lock" outlined @click="advanceMode = 'deposit'" />
          </div>
        </section>

        <section class="block">
          <h3>Чек</h3>
          <template v-if="p.fiscalStatus">
            <StatusTag :label="fiscalStatusLabels[p.fiscalStatus]" :severity="fiscalStatusSeverity[p.fiscalStatus]" />
          </template>
          <p v-else class="hint">Чек не формировался.</p>
        </section>
      </div>

      <section v-if="p.purpose === 'charges'" class="block">
        <h3>Распределение</h3>
        <DataTable :value="activeAllocations" size="small" data-key="id">
          <template #empty>Платёж не распределён на начисления</template>
          <Column header="Начисление">
            <template #body="{ data }">{{ data.chargeTitle }}</template>
          </Column>
          <Column header="Вид">
            <template #body="{ data }">{{ chargeKindLabel(data.chargeKind) }}</template>
          </Column>
          <Column header="Сумма">
            <template #body="{ data }"><MoneyText :kop="data.amountKop" /></template>
          </Column>
        </DataTable>
        <template v-if="reversedAllocations.length">
          <h4>Отменённые распределения</h4>
          <DataTable :value="reversedAllocations" size="small" data-key="id" class="muted">
            <Column header="Начисление" field="chargeTitle" />
            <Column header="Сумма">
              <template #body="{ data }"><MoneyText :kop="data.amountKop" /></template>
            </Column>
            <Column header="Причина" field="reversedReason" />
          </DataTable>
        </template>
      </section>

      <section v-if="p.refunds.length" class="block">
        <h3>Возвраты</h3>
        <DataTable :value="p.refunds" size="small" data-key="id">
          <Column header="Дата">
            <template #body="{ data }">{{ formatDate(data.refundedOn) }}</template>
          </Column>
          <Column header="Сумма">
            <template #body="{ data }"><MoneyText :kop="data.amountKop" /></template>
          </Column>
          <Column header="Способ">
            <template #body="{ data }">{{
              refundMethodLabels[(data as PaymentDetail['refunds'][number]).method]
            }}</template>
          </Column>
          <Column header="Причина" field="reason" />
        </DataTable>
      </section>

      <AdvanceDialog
        v-if="advanceMode"
        v-model:visible="advanceDialog"
        :payment-id="p.id"
        :advance-kop="p.advanceKop"
        :mode="advanceMode"
      />
      <Dialog
        v-model:visible="cancelDialog"
        modal
        header="Отменить платёж"
        :style="{ width: '460px' }"
        :breakpoints="{ '500px': '100vw' }"
      >
        <form class="form" novalidate @submit.prevent="cancelPayment">
          <p class="hint">Распределения платежа будут отменены, начисления снова станут открытыми.</p>
          <div class="field">
            <label for="pc-reason">Причина *</label>
            <Textarea id="pc-reason" v-model="cancelReason" rows="2" auto-resize fluid />
          </div>
          <Message v-if="cancelError" severity="error" :closable="false">{{ cancelError }}</Message>
          <div class="actions">
            <Button type="button" label="Не отменять" outlined @click="cancelDialog = false" />
            <Button type="submit" label="Отменить платёж" severity="danger" :loading="m.cancel.isPending.value" />
          </div>
        </form>
      </Dialog>
    </template>
  </div>
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
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
  margin: 0 0 0.75rem;
}
.props {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.375rem 1rem;
  margin: 0;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
}
.big {
  font-size: 1.25rem;
  font-weight: 600;
  margin: 0 0 0.5rem;
}
.actions--start {
  justify-content: flex-start;
}
.muted {
  color: var(--p-text-muted-color);
}
</style>
