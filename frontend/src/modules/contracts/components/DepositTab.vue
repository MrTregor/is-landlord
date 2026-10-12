<script setup lang="ts">
// Вкладка «Залог» договора (14 Экраны собственника, раздел 4.3, вкладка 4; 05 разд. 9): показатели, операции,
// внесение (регистрация платежа с назначением «Залог»), удержание, зачёт, возврат, отмена последней операции.
import { computed, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import type { ContractDetail } from '@/api/queries/contracts'
import { useDeposit, useDepositMutations, type DepositOperation } from '@/api/queries/payments'
import MoneyText from '@/components/MoneyText.vue'
import { formatDate } from '@/utils/date'
import { depositOperationLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import DepositOperationDialog from '@/modules/payments/components/DepositOperationDialog.vue'
import type { DepositOpKind } from '@/modules/payments/types'
import PaymentDialog from '@/modules/payments/components/PaymentDialog.vue'

const props = defineProps<{ contract: ContractDetail }>()
const toast = useToast()
const deposit = useDeposit(() => props.contract.id)
const d = computed(() => deposit.data.value)
const m = useDepositMutations()
const editable = computed(() => props.contract.status !== 'draft')

const receiptDialog = ref(false)
const opKind = ref<DepositOpKind>('withhold')
const opDialog = ref(false)
function openOp(kind: DepositOpKind) {
  opKind.value = kind
  opDialog.value = true
}

const cancelOp = ref<DepositOperation | null>(null)
const cancelReason = ref('')
const cancelError = ref<string | null>(null)
const cancelDialog = computed({
  get: () => cancelOp.value !== null,
  set: (v: boolean) => {
    if (!v) cancelOp.value = null
  },
})
function openCancel(op: DepositOperation) {
  cancelOp.value = op
  cancelReason.value = ''
  cancelError.value = null
}
async function cancel() {
  if (!cancelReason.value.trim()) {
    cancelError.value = 'Укажите причину'
    return
  }
  try {
    await m.cancel.mutateAsync({ id: cancelOp.value!.id, reason: cancelReason.value.trim() })
    cancelOp.value = null
    toast.add({ severity: 'success', summary: 'Операция отменена', life: 3000 })
  } catch (err) {
    cancelError.value = parseProblem(err).message
  }
}
const op = (x: unknown) => x as DepositOperation
</script>

<template>
  <div>
    <Skeleton v-if="deposit.isPending.value" height="8rem" />
    <Message v-else-if="deposit.isError.value" severity="error" :closable="false">
      {{ parseProblem(deposit.error.value).message }}
    </Message>
    <template v-else-if="d">
      <div class="summary">
        <div>Ожидается: <MoneyText :kop="d.expectedKop" /></div>
        <div>Внесено: <MoneyText :kop="d.receivedKop" /></div>
        <div>
          Остаток: <strong><MoneyText :kop="d.balanceKop" /></strong>
        </div>
      </div>
      <div v-if="editable" class="actions actions--start toolbar">
        <Button label="Внести залог" icon="pi pi-plus" outlined @click="receiptDialog = true" />
        <Button
          label="Удержать"
          icon="pi pi-minus-circle"
          outlined
          :disabled="d.balanceKop <= 0"
          @click="openOp('withhold')"
        />
        <Button
          label="Зачесть в счёт долга"
          icon="pi pi-arrow-right-arrow-left"
          outlined
          :disabled="d.balanceKop <= 0 || contract.balances.debtKop <= 0"
          @click="openOp('offset')"
        />
        <Button label="Вернуть" icon="pi pi-replay" outlined :disabled="d.balanceKop <= 0" @click="openOp('refund')" />
      </div>
      <DataTable
        :value="d.operations"
        size="small"
        data-key="id"
        :row-class="(r: DepositOperation) => (r.cancelledAt ? 'row--cancelled' : '')"
      >
        <template #empty>Операций нет</template>
        <Column header="Дата">
          <template #body="{ data }">{{ formatDate(op(data).opDate) }}</template>
        </Column>
        <Column header="Вид">
          <template #body="{ data }">
            {{ depositOperationLabels[op(data).kind] }}
            <span v-if="op(data).fromAdvance" class="hint">(из переплаты)</span>
          </template>
        </Column>
        <Column header="Сумма">
          <template #body="{ data }"><MoneyText :kop="op(data).amountKop" /></template>
        </Column>
        <Column header="Основание">
          <template #body="{ data }">
            {{ op(data).reason ?? '—' }}
            <div v-if="op(data).cancelledAt" class="hint">Отменена: {{ op(data).cancelReason }}</div>
          </template>
        </Column>
        <Column header="Связано с">
          <template #body="{ data }">
            <RouterLink v-if="op(data).paymentId" :to="`/payments/${op(data).paymentId}`">платёж</RouterLink>
            <span v-else class="hint">—</span>
          </template>
        </Column>
        <Column header="Кем">
          <template #body="{ data }">{{ op(data).createdBy ?? '—' }}</template>
        </Column>
        <Column header="">
          <template #body="{ data }">
            <Button
              v-if="editable && op(data).canCancel"
              label="Отменить"
              size="small"
              text
              severity="danger"
              @click="openCancel(op(data))"
            />
          </template>
        </Column>
      </DataTable>

      <PaymentDialog v-model:visible="receiptDialog" :contract-id="contract.id" purpose="deposit" />
      <DepositOperationDialog
        v-model:visible="opDialog"
        :contract-id="contract.id"
        :balance-kop="d.balanceKop"
        :kind="opKind"
      />
      <Dialog
        v-model:visible="cancelDialog"
        modal
        header="Отменить операцию"
        :style="{ width: '460px' }"
        :breakpoints="{ '500px': '100vw' }"
      >
        <form class="form" novalidate @submit.prevent="cancel">
          <p v-if="cancelOp" class="hint">
            {{ depositOperationLabels[cancelOp.kind] }} на <MoneyText :kop="cancelOp.amountKop" /> от
            {{ formatDate(cancelOp.opDate) }}.
            <template
              v-if="
                cancelOp.kind === 'offset' ||
                (cancelOp.kind === 'receipt' && cancelOp.paymentId && !cancelOp.fromAdvance)
              "
            >
              Связанный платёж тоже будет отменён.
            </template>
          </p>
          <div class="field">
            <label for="dc-reason">Причина *</label>
            <Textarea id="dc-reason" v-model="cancelReason" rows="2" auto-resize fluid />
          </div>
          <Message v-if="cancelError" severity="error" :closable="false">{{ cancelError }}</Message>
          <div class="actions">
            <Button type="button" label="Не отменять" outlined @click="cancelDialog = false" />
            <Button type="submit" label="Отменить операцию" severity="danger" :loading="m.cancel.isPending.value" />
          </div>
        </form>
      </Dialog>
    </template>
  </div>
</template>

<style scoped>
.summary {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 2rem;
  margin-bottom: 0.75rem;
}
.toolbar {
  margin-bottom: 0.75rem;
}
.actions--start {
  justify-content: flex-start;
}
:deep(.row--cancelled) {
  color: var(--p-text-muted-color);
  text-decoration: line-through;
}
</style>
