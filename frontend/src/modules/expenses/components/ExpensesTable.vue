<script setup lang="ts">
// Таблица затрат (14 Экраны собственника, раздел 10): Дата оплаты, Объект, Категория, Описание, Контрагент, Сумма,
// Месяц отнесения, Заявка, Документ, Возмещение; итог по сумме. Изменение и удаление (только без возмещения).
import { ref } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { useExpenseMutations, type Expense } from '@/api/queries/maintenance'
import MoneyText from '@/components/MoneyText.vue'
import ChargeDrawer from '@/modules/charges/components/ChargeDrawer.vue'
import { formatDate, formatMonth } from '@/utils/date'
import { expenseCategoryLabels, expenseRecoveryLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import ExpenseDialog from './ExpenseDialog.vue'

defineProps<{
  items: Expense[]
  total: number
  totalKop: number
  loading?: boolean
  first?: number
  rows?: number
  paginator?: boolean
  hideProperty?: boolean
}>()
const emit = defineEmits<{ page: [page: number, rows: number] }>()
const toast = useToast()
const confirm = useConfirm()
const m = useExpenseMutations()
const e = (x: unknown) => x as Expense

const editing = ref<Expense | null>(null)
const dialog = ref(false)
function edit(x: Expense) {
  editing.value = x
  dialog.value = true
}
const chargeId = ref<string | null>(null)

function remove(x: Expense) {
  confirm.require({
    header: 'Удалить затрату?',
    message: `${x.description} — затрата будет удалена (запись останется в журнале действий).`,
    acceptProps: { label: 'Удалить', severity: 'danger' },
    rejectProps: { label: 'Отмена', outlined: true },
    accept: async () => {
      try {
        await m.remove.mutateAsync(x.id)
        toast.add({ severity: 'success', summary: 'Затрата удалена', life: 3000 })
      } catch (err) {
        toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
      }
    },
  })
}
</script>

<template>
  <div>
    <DataTable
      :value="items"
      :loading="loading"
      :lazy="paginator"
      :paginator="paginator"
      :first="first ?? 0"
      :rows="rows ?? 25"
      :total-records="total"
      :rows-per-page-options="[25, 50, 100]"
      size="small"
      striped-rows
      data-key="id"
      @page="(ev) => emit('page', ev.page + 1, ev.rows)"
    >
      <template #empty>Затрат нет</template>
      <Column header="Дата оплаты">
        <template #body="{ data }">{{ formatDate(e(data).paidOn) }}</template>
      </Column>
      <Column v-if="!hideProperty" header="Объект">
        <template #body="{ data }">
          <RouterLink :to="`/properties/${e(data).property.id}`">{{ e(data).property.code }}</RouterLink>
        </template>
      </Column>
      <Column header="Категория">
        <template #body="{ data }">{{ expenseCategoryLabels[e(data).category] }}</template>
      </Column>
      <Column header="Описание">
        <template #body="{ data }">{{ e(data).description }}</template>
      </Column>
      <Column header="Контрагент">
        <template #body="{ data }">{{ e(data).counterparty ?? '—' }}</template>
      </Column>
      <Column header="Сумма" class="num">
        <template #body="{ data }"><MoneyText :kop="e(data).amountKop" /></template>
      </Column>
      <Column header="Месяц отнесения">
        <template #body="{ data }">{{ formatMonth(e(data).periodMonth) }}</template>
      </Column>
      <Column header="Заявка">
        <template #body="{ data }">
          <RouterLink v-if="e(data).maintenanceRequest" :to="`/requests/${e(data).maintenanceRequest!.id}`">
            {{ e(data).maintenanceRequest!.number }}
          </RouterLink>
          <span v-else>—</span>
        </template>
      </Column>
      <Column header="Документ">
        <template #body="{ data }">
          <a
            v-if="e(data).file"
            :href="e(data).file!.url"
            target="_blank"
            rel="noopener"
            :aria-label="`Документ ${e(data).file!.name}`"
            :title="e(data).file!.name"
            ><i class="pi pi-file" aria-hidden="true"
          /></a>
        </template>
      </Column>
      <Column header="Возмещение">
        <template #body="{ data }">
          <Button
            v-if="e(data).recovery === 'charge' && e(data).chargeId"
            :label="expenseRecoveryLabels.charge"
            link
            size="small"
            class="link"
            @click="chargeId = e(data).chargeId"
          />
          <span v-else>{{ expenseRecoveryLabels[e(data).recovery] }}</span>
        </template>
      </Column>
      <Column header="" class="col-actions">
        <template #body="{ data }">
          <div class="row-actions">
            <Button icon="pi pi-pencil" text size="small" aria-label="Изменить" @click="edit(e(data))" />
            <Button
              v-if="e(data).recovery === 'none'"
              icon="pi pi-trash"
              text
              size="small"
              severity="danger"
              aria-label="Удалить"
              @click="remove(e(data))"
            />
          </div>
        </template>
      </Column>
      <template #footer>
        <div class="total">Итого: <MoneyText :kop="totalKop" /></div>
      </template>
    </DataTable>
    <ExpenseDialog v-if="editing" v-model:visible="dialog" :expense="editing" />
    <ChargeDrawer v-model:charge-id="chargeId" />
  </div>
</template>

<style scoped>
.total {
  text-align: right;
  font-weight: 600;
}
.row-actions {
  display: flex;
  gap: 0.25rem;
}
.link {
  padding: 0;
}
</style>
