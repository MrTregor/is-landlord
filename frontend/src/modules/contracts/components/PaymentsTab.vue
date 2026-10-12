<script setup lang="ts">
// Вкладка «Платежи» договора (14 Экраны собственника, раздел 4.3, вкладка 3).
import { computed, ref } from 'vue'
import type { ContractDetail } from '@/api/queries/contracts'
import type { PaymentsQuery } from '@/api/queries/payments'
import PaymentDialog from '@/modules/payments/components/PaymentDialog.vue'
import PaymentsTable from '@/modules/payments/components/PaymentsTable.vue'

const props = defineProps<{ contract: ContractDetail }>()
const page = ref(1)
const pageSize = ref(25)
const sort = ref<PaymentsQuery['sort']>()
const query = computed<PaymentsQuery>(() => ({
  contractId: props.contract.id,
  page: page.value,
  pageSize: pageSize.value,
  sort: sort.value,
}))
const dialog = ref(false)
</script>

<template>
  <div>
    <div v-if="contract.status !== 'draft'" class="actions actions--start toolbar">
      <Button label="Зарегистрировать платёж" icon="pi pi-plus" outlined @click="dialog = true" />
    </div>
    <PaymentsTable
      :query="query"
      @page="
        (p, s) => {
          page = p
          pageSize = s
        }
      "
      @sort="(s) => (sort = s as PaymentsQuery['sort'])"
    />
    <PaymentDialog v-model:visible="dialog" :contract-id="contract.id" />
  </div>
</template>

<style scoped>
.toolbar {
  margin-bottom: 0.75rem;
}
.actions--start {
  justify-content: flex-start;
}
</style>
