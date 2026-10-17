<script setup lang="ts">
// Договор и документы (15 Экраны нанимателя, раздел 7): условия, проживающие, документы (PDF в новой вкладке),
// подписанные допсоглашения и история платы. Для договоров аренды (ch34) — «арендная плата».
import { computed } from 'vue'
import { useCabinetContract } from '@/api/queries/cabinet'
import MoneyText from '@/components/MoneyText.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, formatDateTime } from '@/utils/date'
import {
  amendmentKindLabels,
  contractStatusLabels,
  contractStatusSeverity,
  documentKindLabels,
  legalBasisLabels,
  utilitiesModeLabels,
} from '@/utils/labels'
import { formatPct } from '@/utils/money'
import { parseProblem } from '@/utils/problem'
import { useCabinetContractId } from '../useContract'

const { summary, contractId, contractOptions, selectContract } = useCabinetContractId()
const contract = useCabinetContract(contractId)
const c = computed(() => contract.data.value)
const rentTitle = computed(() => (c.value?.legalBasis === 'ch34' ? 'Арендная плата' : 'Плата за наём'))
const penalty = computed(() => {
  const v = c.value
  if (!v?.penaltyEnabled) return 'Не начисляются'
  const parts = [`${formatPct(v.penaltyRatePct)} в день от суммы долга`]
  if (v.penaltyGraceDays > 0) parts.push(`после ${v.penaltyGraceDays} дн. со срока оплаты`)
  if (v.penaltyCapPct) parts.push(`не более ${formatPct(v.penaltyCapPct)} суммы долга`)
  if (v.penaltyOnUtilities) parts.push('в том числе на ЖКУ')
  return parts.join(', ')
})
const indexation = computed(() => {
  const v = c.value
  if (!v?.indexationEnabled) return 'Не предусмотрена'
  return `${formatPct(v.indexationPct)} раз в ${v.indexationPeriodMonths} мес.`
})
const period = computed(() => {
  const v = c.value
  if (!v) return ''
  const end = v.actualEndDate !== v.endDate ? `${formatDate(v.actualEndDate)} (досрочно)` : formatDate(v.endDate)
  return `${formatDate(v.startDate)} – ${end}`
})
</script>

<template>
  <div class="contract">
    <Select
      v-if="contractOptions.length > 1"
      :model-value="contractId"
      :options="contractOptions"
      option-label="label"
      option-value="value"
      aria-label="Договор"
      fluid
      @update:model-value="selectContract"
    />
    <Message v-if="summary.isError.value || contract.isError.value" severity="error" :closable="false">
      {{ parseProblem(summary.error.value ?? contract.error.value).message }}
    </Message>
    <Message v-else-if="summary.data.value && !contractId" severity="info" :closable="false"
      >Договоров пока нет.</Message
    >
    <Skeleton v-else-if="!c" height="12rem" />
    <template v-else>
      <header>
        <h1 class="title">
          {{ legalBasisLabels[c.legalBasis] }} <span class="nowrap">№ {{ c.number }}</span>
        </h1>
        <StatusTag :label="contractStatusLabels[c.status]" :severity="contractStatusSeverity[c.status]" />
        <p class="hint">{{ c.propertyAddress }}</p>
      </header>

      <section class="card">
        <h2>Условия</h2>
        <dl class="terms">
          <dt>Срок</dt>
          <dd>{{ period }}</dd>
          <dt>{{ rentTitle }}</dt>
          <dd><MoneyText :kop="c.currentTerms.rentKop" /> в месяц</dd>
          <dt>День оплаты</dt>
          <dd>до {{ c.currentTerms.paymentDay }} числа</dd>
          <dt>Обеспечительный платёж</dt>
          <dd><MoneyText v-if="c.depositKop > 0" :kop="c.depositKop" /><template v-else>Нет</template></dd>
          <dt>Коммунальные платежи</dt>
          <dd>
            {{ utilitiesModeLabels[c.utilitiesMode]
            }}<template v-if="c.currentTerms.utilitiesFixedKop > 0"
              >, фиксированная часть <MoneyText :kop="c.currentTerms.utilitiesFixedKop"
            /></template>
          </dd>
          <dt>Пени</dt>
          <dd>{{ penalty }}</dd>
          <dt>Индексация</dt>
          <dd>{{ indexation }}</dd>
          <template v-if="c.specialTerms">
            <dt>Особые условия</dt>
            <dd class="text">{{ c.specialTerms }}</dd>
          </template>
        </dl>
      </section>

      <section v-if="c.occupants.length" class="card">
        <h2>Проживающие</h2>
        <ul class="list">
          <li v-for="o in c.occupants" :key="o.id">
            {{ o.fullName }}<span v-if="o.relation" class="hint">, {{ o.relation }}</span>
          </li>
        </ul>
      </section>

      <section class="card">
        <h2>Документы</h2>
        <p v-if="!c.documents.length" class="hint">Документов пока нет</p>
        <ul v-else class="list">
          <li v-for="d in c.documents" :key="d.id" class="doc">
            <span>
              {{ d.title }}
              <span class="hint">{{ documentKindLabels[d.kind] }} · {{ formatDateTime(d.createdAt) }}</span>
            </span>
            <Button
              as="a"
              :href="d.pdfUrl"
              target="_blank"
              rel="noopener"
              label="Открыть"
              icon="pi pi-file-pdf"
              outlined
              size="small"
              class="open"
            />
          </li>
        </ul>
      </section>

      <section v-if="c.amendments.length" class="card">
        <h2>Дополнительные соглашения</h2>
        <ul class="list">
          <li v-for="a in c.amendments" :key="a.id">
            № {{ a.number }} — {{ amendmentKindLabels[a.kind] }}, с {{ formatDate(a.effectiveFrom) }}
            <span v-if="a.signedOn" class="hint">(подписано {{ formatDate(a.signedOn) }})</span>
          </li>
        </ul>
      </section>

      <section v-if="c.termsHistory.length > 1" class="card">
        <h2>История изменений платы</h2>
        <ul class="list">
          <li v-for="t in c.termsHistory" :key="t.version" class="row">
            <span>с {{ formatDate(t.effectiveFrom) }}</span>
            <MoneyText :kop="t.rentKop" />
          </li>
        </ul>
      </section>
    </template>
  </div>
</template>

<style scoped>
.contract {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.title {
  font-size: 1.25rem;
  margin: 0 0 0.5rem;
}
header p {
  margin: 0.5rem 0 0;
}
.card {
  background: var(--p-content-background);
  border: 1px solid var(--p-content-border-color);
  border-radius: 8px;
  padding: 0.75rem 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-width: 0;
}
.card h2 {
  margin: 0;
  font-size: 1rem;
  color: var(--p-text-muted-color);
}
.terms {
  display: grid;
  grid-template-columns: minmax(0, 11rem) minmax(0, 1fr);
  gap: 0.5rem 1rem;
  margin: 0;
}
.terms dt {
  color: var(--p-text-muted-color);
}
.terms dd {
  margin: 0;
  overflow-wrap: anywhere;
}
@media (max-width: 479.98px) {
  .terms {
    grid-template-columns: minmax(0, 1fr);
    gap: 0.125rem;
  }
  .terms dd {
    margin-bottom: 0.5rem;
  }
}
.text {
  white-space: pre-wrap;
}
.nowrap {
  white-space: nowrap;
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.list li {
  padding: 0.375rem 0;
  border-bottom: 1px solid var(--p-content-border-color);
  overflow-wrap: anywhere;
}
.doc,
.row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.75rem;
}
.doc .hint {
  display: block;
}
.open {
  flex-shrink: 0;
  min-height: 44px;
}
</style>
