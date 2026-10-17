<script setup lang="ts">
// Карточка объекта → «Затраты» (14 Экраны собственника, раздел 2.3): затраты объекта и «Добавить затрату».
import { ref } from 'vue'
import { useExpenses } from '@/api/queries/maintenance'
import ExpenseDialog from '@/modules/expenses/components/ExpenseDialog.vue'
import ExpensesTable from '@/modules/expenses/components/ExpensesTable.vue'

const props = defineProps<{ propertyId: string }>()
const page = ref(1)
const pageSize = ref(25)
const list = useExpenses(() => ({ propertyId: props.propertyId, page: page.value, pageSize: pageSize.value }))
const dialog = ref(false)
</script>

<template>
  <div>
    <div class="actions actions--start">
      <Button label="Добавить затрату" icon="pi pi-plus" @click="dialog = true" />
    </div>
    <ExpensesTable
      :items="list.data.value?.items ?? []"
      :total="list.data.value?.total ?? 0"
      :total-kop="list.data.value?.summary.amountKop ?? 0"
      :loading="list.isFetching.value && !list.data.value"
      :first="(page - 1) * pageSize"
      :rows="pageSize"
      paginator
      hide-property
      @page="
        (p, r) => {
          page = p
          pageSize = r
        }
      "
    />
    <ExpenseDialog v-model:visible="dialog" :property-id="propertyId" />
  </div>
</template>
