<script setup lang="ts">
// Платежи (14 Экраны собственника, раздел 6.1): фильтры в адресной строке, таблица, регистрация платежа.
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { PaymentsQuery } from '@/api/queries/payments'
import DateInput from '@/components/DateInput.vue'
import PageHeader from '@/components/PageHeader.vue'
import { options, paymentMethodLabels, paymentSourceLabels, paymentStatusLabels } from '@/utils/labels'
import { useListQuery } from '@/utils/listQuery'
import PaymentDialog from '../components/PaymentDialog.vue'
import PaymentsTable from '../components/PaymentsTable.vue'

const router = useRouter()
const { state, set, reset } = useListQuery()
const str = (k: string) => (state.value.values[k] as string | undefined) || undefined
const query = computed<PaymentsQuery>(() => ({
  q: str('q'),
  status: str('status') as PaymentsQuery['status'],
  method: str('method') as PaymentsQuery['method'],
  source: str('source') as PaymentsQuery['source'],
  from: str('from'),
  to: str('to'),
  sort: state.value.sort as PaymentsQuery['sort'],
  page: state.value.page,
  pageSize: state.value.pageSize,
}))

const search = ref(str('q') ?? '')
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => set({ q: v.trim() || undefined }), 300)
})

const dialog = ref(false)
</script>

<template>
  <div>
    <PageHeader title="Платежи">
      <template #actions>
        <Button label="Зарегистрировать платёж" icon="pi pi-plus" @click="dialog = true" />
      </template>
    </PageHeader>

    <div class="filters">
      <IconField>
        <InputIcon class="pi pi-search" />
        <InputText v-model="search" placeholder="Квитанция, плательщик, договор" aria-label="Поиск" />
      </IconField>
      <Select
        :model-value="state.values.status"
        :options="options(paymentStatusLabels)"
        option-label="label"
        option-value="value"
        placeholder="Статус"
        aria-label="Статус"
        show-clear
        @update:model-value="(v) => set({ status: v ?? undefined })"
      />
      <Select
        :model-value="state.values.method"
        :options="options(paymentMethodLabels)"
        option-label="label"
        option-value="value"
        placeholder="Способ"
        aria-label="Способ"
        show-clear
        @update:model-value="(v) => set({ method: v ?? undefined })"
      />
      <Select
        :model-value="state.values.source"
        :options="options(paymentSourceLabels)"
        option-label="label"
        option-value="value"
        placeholder="Источник"
        aria-label="Источник"
        show-clear
        @update:model-value="(v) => set({ source: v ?? undefined })"
      />
      <span class="hint">Поступление:</span>
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
      <Button
        label="Сбросить"
        text
        @click="
          () => {
            search = ''
            reset()
          }
        "
      />
    </div>

    <PaymentsTable
      :query="query"
      show-contract
      @page="(p, s) => set({ page: p, pageSize: s }, false)"
      @sort="(s) => set({ sort: s }, false)"
    />
    <PaymentDialog v-model:visible="dialog" @created="(p) => router.push(`/payments/${p.id}`)" />
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
</style>
