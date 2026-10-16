<script setup lang="ts">
// Выписка банка (14 Экраны собственника, раздел 7.1): строки, фильтр по статусу; для «Возможный дубль» —
// «Это тот же платёж» (выбор из найденных) и «Это другой платёж» (05 разд. 15.1, шаг 5).
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { useBankStatement, useStatementMutations, type BankLine } from '@/api/queries/reconciliation'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import {
  bankLineStatusLabels,
  bankLineStatusSeverity,
  matchMethodLabels,
  options,
  paymentSourceLabels,
  statementFormatLabels,
} from '@/utils/labels'
import { formatKop } from '@/utils/money'
import { parseProblem } from '@/utils/problem'

type LineStatus = BankLine['status']

const route = useRoute()
const router = useRouter()
const toast = useToast()
const m = useStatementMutations()

const id = computed(() => route.params.id as string)
const status = computed<LineStatus | undefined>({
  get: () => (route.query.status as LineStatus | undefined) || undefined,
  set: (v) => void router.replace({ query: v ? { status: v } : {} }),
})
const st = useBankStatement(id, status)
const lines = computed(() => st.data.value?.lines ?? [])
const l = (x: unknown) => x as BankLine

const pending = ref<string | null>(null)
async function run(line: BankLine, fn: () => Promise<unknown>, done: string) {
  pending.value = line.id
  try {
    await fn()
    toast.add({ severity: 'success', summary: done, life: 4000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: parseProblem(e).message, life: 6000 })
  } finally {
    pending.value = null
  }
}
const same = (line: BankLine, paymentId: string) =>
  run(line, () => m.samePayment.mutateAsync({ lineId: line.id, paymentId }), 'Строка связана с платежом')
const different = (line: BankLine) =>
  run(line, () => m.differentPayment.mutateAsync(line.id), 'Платёж по строке выписки создан')
</script>

<template>
  <div>
    <PageHeader :title="st.data.value ? `Выписка: ${st.data.value.fileName}` : 'Выписка банка'">
      <template #actions>
        <Button
          label="К загрузкам"
          icon="pi pi-arrow-left"
          text
          @click="router.push('/reconciliation?tab=statements')"
        />
      </template>
    </PageHeader>
    <Message v-if="st.isError.value" severity="error" :closable="false">{{
      parseProblem(st.error.value).message
    }}</Message>
    <template v-if="st.data.value">
      <p class="summary">
        {{ statementFormatLabels[st.data.value.format] }} · загружена {{ formatDateTime(st.data.value.uploadedAt) }}
        <template v-if="st.data.value.uploadedBy">({{ st.data.value.uploadedBy }})</template>
        <template v-if="st.data.value.accountNumber"> · счёт {{ st.data.value.accountNumber }}</template>
        <template v-if="st.data.value.dateFrom || st.data.value.dateTo">
          · период {{ formatDate(st.data.value.dateFrom) || '…' }} – {{ formatDate(st.data.value.dateTo) || '…' }}
        </template>
        <br />
        Строк: {{ st.data.value.linesTotal }}, входящих: {{ st.data.value.linesIncoming }}, сопоставлено:
        {{ st.data.value.linesMatched }}, не опознано: {{ st.data.value.linesUnmatched }}, дублей:
        {{ st.data.value.linesDuplicate }}
      </p>
      <div class="filters">
        <Select
          v-model="status"
          :options="options(bankLineStatusLabels)"
          option-label="label"
          option-value="value"
          placeholder="Статус строки"
          aria-label="Статус строки"
          show-clear
        />
        <RouterLink v-if="st.data.value.linesUnmatched" to="/reconciliation?kind=unidentified">
          Разобрать неопознанные платежи
        </RouterLink>
      </div>
    </template>
    <DataTable :value="lines" :loading="st.isFetching.value" size="small" striped-rows data-key="id" scrollable>
      <template #empty>Строк нет</template>
      <Column header="№" field="lineNo" />
      <Column header="Дата поступления">
        <template #body="{ data }">{{ formatDate(l(data).receivedOn) }}</template>
      </Column>
      <Column header="Сумма">
        <template #body="{ data }"><MoneyText :kop="l(data).amountKop" /></template>
      </Column>
      <Column header="Плательщик">
        <template #body="{ data }">{{ l(data).payerName ?? '—' }}</template>
      </Column>
      <Column header="ИНН">
        <template #body="{ data }">{{ l(data).payerInn ?? '—' }}</template>
      </Column>
      <Column header="Назначение" class="purpose">
        <template #body="{ data }">{{ l(data).purpose ?? '—' }}</template>
      </Column>
      <Column header="Статус строки">
        <template #body="{ data }">
          <StatusTag :label="bankLineStatusLabels[l(data).status]" :severity="bankLineStatusSeverity[l(data).status]" />
          <div v-if="l(data).note" class="hint">{{ l(data).note }}</div>
        </template>
      </Column>
      <Column header="Способ сопоставления">
        <template #body="{ data }">{{ l(data).matchMethod ? matchMethodLabels[l(data).matchMethod!] : '—' }}</template>
      </Column>
      <Column header="Договор">
        <template #body="{ data }">
          <RouterLink v-if="l(data).contract" :to="`/contracts/${l(data).contract!.id}`">
            № {{ l(data).contract!.number }}
          </RouterLink>
          <span v-else>—</span>
        </template>
      </Column>
      <Column header="Платёж">
        <template #body="{ data }">
          <RouterLink v-if="l(data).paymentId" :to="`/payments/${l(data).paymentId}`">Открыть</RouterLink>
          <div v-else-if="l(data).status === 'possible_duplicate'" class="dup">
            <Button
              v-for="c in l(data).candidatePayments"
              :key="c.id"
              :label="`Это тот же платёж: ${c.receiptNumber ?? ''} от ${formatDate(c.receivedOn)}, ${formatKop(c.amountKop)} (${paymentSourceLabels[c.source]})`"
              size="small"
              outlined
              :loading="pending === l(data).id && m.samePayment.isPending.value"
              :disabled="pending !== null"
              @click="same(l(data), c.id)"
            />
            <Button
              label="Это другой платёж"
              size="small"
              outlined
              severity="secondary"
              :loading="pending === l(data).id && m.differentPayment.isPending.value"
              :disabled="pending !== null"
              @click="different(l(data))"
            />
          </div>
          <span v-else>—</span>
        </template>
      </Column>
    </DataTable>
  </div>
</template>

<style scoped>
.summary {
  margin: 0 0 0.75rem;
  color: var(--p-text-muted-color);
}
.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 1rem;
  margin-bottom: 0.75rem;
}
.dup {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.25rem;
}
:deep(.purpose) {
  min-width: 14rem;
}
</style>
