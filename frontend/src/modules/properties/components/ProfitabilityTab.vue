<script setup lang="ts">
// Карточка объекта → «Доходность» (14 Экраны собственника, раздел 2.3; 05 разд. 2.3): поступления, расходы,
// результат, рентабельность, дни сдачи и простоя, недополученный доход, разбивка по месяцам.
import dayjs from 'dayjs'
import { ref } from 'vue'
import { useProfitability } from '@/api/queries/reports'
import MoneyText from '@/components/MoneyText.vue'
import PeriodPicker from '@/modules/reports/components/PeriodPicker.vue'
import { presetPeriod } from '@/modules/reports/period'
import { formatMonth } from '@/utils/date'
import { formatPct } from '@/utils/money'
import { parseProblem } from '@/utils/problem'

const props = defineProps<{ propertyId: string }>()
const period = ref(presetPeriod('year', dayjs().format('YYYY-MM-DD')))
const prof = useProfitability(() => props.propertyId, period)
</script>

<template>
  <div>
    <PeriodPicker v-model="period" />
    <Message v-if="prof.isError.value" severity="error" :closable="false">
      {{ parseProblem(prof.error.value).message }}
    </Message>
    <template v-if="prof.data.value">
      <dl class="props">
        <dt>Поступления</dt>
        <dd><MoneyText :kop="prof.data.value.incomeKop" /></dd>
        <dt>Расходы</dt>
        <dd><MoneyText :kop="prof.data.value.expensesKop" /></dd>
        <dt>Финансовый результат</dt>
        <dd>
          <b><MoneyText :kop="prof.data.value.resultKop" /></b>
        </dd>
        <dt>Рентабельность</dt>
        <dd>{{ formatPct(prof.data.value.marginPct) }}</dd>
        <dt>Заполняемость</dt>
        <dd>
          {{ formatPct(prof.data.value.occupancyPct) }} · сдан {{ prof.data.value.rentedDays }} дн., простой
          {{ prof.data.value.idleDays }} дн. (свободен {{ prof.data.value.idleByStatus.free }}, забронирован
          {{ prof.data.value.idleByStatus.reserved }}, на ремонте {{ prof.data.value.idleByStatus.repair }})
        </dd>
        <dt>Недополученный доход</dt>
        <dd><MoneyText :kop="prof.data.value.lostIncomeKop" /></dd>
      </dl>
      <DataTable :value="prof.data.value.byMonth" size="small" striped-rows>
        <Column header="Месяц"
          ><template #body="{ data }">{{ formatMonth(data.month) }}</template></Column
        >
        <Column header="Поступления"
          ><template #body="{ data }"><MoneyText :kop="data.incomeKop" /></template
        ></Column>
        <Column header="Расходы"
          ><template #body="{ data }"><MoneyText :kop="data.expensesKop" /></template
        ></Column>
        <Column header="Сдан, дн." field="rentedDays" />
        <Column header="Простой, дн." field="idleDays" />
      </DataTable>
    </template>
  </div>
</template>

<style scoped>
.props {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 0.375rem 1rem;
  margin: 1rem 0;
}
.props dt {
  color: var(--p-text-muted-color);
}
.props dd {
  margin: 0;
}
</style>
