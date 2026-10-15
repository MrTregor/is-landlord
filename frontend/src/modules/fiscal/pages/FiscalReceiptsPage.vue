<script setup lang="ts">
// Чеки (14 Экраны собственника, раздел 13): фильтры «Статус» и «Период», таблица чеков; «Открыть чек» (ссылка ОФД
// или «Мой налог»), «Повторить» (ошибка), «Указать ссылку на чек НПД».
import { computed, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useFiscalMutations, useFiscalReceipts, type FiscalQuery, type FiscalReceipt } from '@/api/queries/fiscal'
import DateInput from '@/components/DateInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDateTime } from '@/utils/date'
import { fiscalOperationLabels, fiscalStatusLabels, fiscalStatusSeverity, options } from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'
import { parseProblem } from '@/utils/problem'
import ManualReceiptDialog from '../components/ManualReceiptDialog.vue'

const toast = useToast()
const { state, set, reset } = useListQuery()
const str = (k: string) => (state.value.values[k] as string | undefined) || undefined
const query = computed<FiscalQuery>(() => ({
  status: str('status') as FiscalQuery['status'],
  from: str('from'),
  to: str('to'),
  page: state.value.page,
  pageSize: state.value.pageSize,
}))
const list = useFiscalReceipts(query)
const m = useFiscalMutations()
const r = (d: unknown) => d as FiscalReceipt
const link = (f: FiscalReceipt) => f.ofdReceiptUrl ?? f.npdReceiptUrl

const retrying = ref<string | null>(null)
async function retry(f: FiscalReceipt) {
  retrying.value = f.id
  try {
    await m.retry.mutateAsync(f.id)
    toast.add({ severity: 'info', summary: 'Чек отправлен повторно', life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(e).message, life: 6000 })
  } finally {
    retrying.value = null
  }
}

const manualId = ref<string | null>(null)
const manualDialog = computed({
  get: () => manualId.value !== null,
  set: (v: boolean) => {
    if (!v) manualId.value = null
  },
})
</script>

<template>
  <div>
    <PageHeader title="Чеки" />

    <div class="filters">
      <Select
        :model-value="state.values.status"
        :options="options(fiscalStatusLabels)"
        option-label="label"
        option-value="value"
        placeholder="Статус"
        aria-label="Статус"
        show-clear
        @update:model-value="(v) => set({ status: v ?? undefined })"
      />
      <span class="hint">Период:</span>
      <div class="date">
        <DateInput
          :model-value="str('from') ?? null"
          placeholder="с"
          @update:model-value="(v) => set({ from: v ?? undefined })"
        />
      </div>
      <div class="date">
        <DateInput
          :model-value="str('to') ?? null"
          placeholder="по"
          @update:model-value="(v) => set({ to: v ?? undefined })"
        />
      </div>
      <Button label="Сбросить" text @click="reset()" />
    </div>

    <Message v-if="list.isError.value" severity="error" :closable="false">
      {{ parseProblem(list.error.value).message }}
    </Message>
    <DataTable
      :value="list.data.value?.items ?? []"
      :loading="list.isFetching.value && !list.data.value"
      lazy
      paginator
      :first="((query.page ?? 1) - 1) * (query.pageSize ?? 25)"
      :rows="query.pageSize ?? 25"
      :total-records="list.data.value?.total ?? 0"
      :rows-per-page-options="[25, 50, 100]"
      size="small"
      striped-rows
      data-key="id"
      @page="(e) => set({ page: e.page + 1, pageSize: e.rows }, false)"
    >
      <template #empty>Чеков нет</template>
      <Column header="Создан">
        <template #body="{ data }">{{ formatDateTime(r(data).createdAt) }}</template>
      </Column>
      <Column header="Операция">
        <template #body="{ data }">{{ fiscalOperationLabels[r(data).operation] }}</template>
      </Column>
      <Column header="Платёж">
        <template #body="{ data }">
          <RouterLink v-if="r(data).paymentId" :to="`/payments/${r(data).paymentId}`">
            {{ r(data).receiptNumber ?? 'Платёж' }}
          </RouterLink>
          <span v-else>{{ r(data).receiptNumber ?? '—' }}</span>
        </template>
      </Column>
      <Column header="Договор">
        <template #body="{ data }">
          <RouterLink v-if="r(data).contract" :to="`/contracts/${r(data).contract!.id}`">
            № {{ r(data).contract!.number }}
          </RouterLink>
          <span v-else>—</span>
        </template>
      </Column>
      <Column header="Сумма">
        <template #body="{ data }"><MoneyText :kop="r(data).totalKop" /></template>
      </Column>
      <Column header="Статус">
        <template #body="{ data }">
          <StatusTag :label="fiscalStatusLabels[r(data).status]" :severity="fiscalStatusSeverity[r(data).status]" />
        </template>
      </Column>
      <Column header="ФД / ФП">
        <template #body="{ data }">
          <span v-if="r(data).fiscalDocumentNumber"
            >{{ r(data).fiscalDocumentNumber }} / {{ r(data).fiscalDocumentAttribute }}</span
          >
          <span v-else>—</span>
        </template>
      </Column>
      <Column header="Ошибка">
        <template #body="{ data }">
          <span class="error">{{ r(data).errorText ?? '' }}</span>
        </template>
      </Column>
      <Column header="" class="col-actions">
        <template #body="{ data }">
          <div class="row-actions">
            <Button
              v-if="link(r(data))"
              as="a"
              :href="link(r(data))!"
              target="_blank"
              rel="noopener"
              label="Открыть чек"
              icon="pi pi-external-link"
              size="small"
              text
            />
            <Button
              v-if="r(data).status === 'fail'"
              label="Повторить"
              icon="pi pi-refresh"
              size="small"
              text
              :loading="retrying === r(data).id"
              @click="retry(r(data))"
            />
            <Button
              v-if="r(data).status === 'manual_required'"
              label="Указать ссылку на чек НПД"
              icon="pi pi-link"
              size="small"
              text
              @click="manualId = r(data).id"
            />
          </div>
        </template>
      </Column>
    </DataTable>
    <ManualReceiptDialog v-if="manualId" v-model:visible="manualDialog" :receipt-id="manualId" />
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}
.date {
  width: 11rem;
}
.error {
  color: var(--p-red-600);
}
.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
  justify-content: flex-end;
}
</style>
