<script setup lang="ts">
// Страница отчёта (14 Экраны собственника, раздел 12): параметры сверху (период с быстрыми кнопками, год или дата),
// «Построить», таблица результата (как первый лист XLSX; для налоговой базы — вкладки «Сводка» и «Операции»),
// «Выгрузить в Excel».
import dayjs from 'dayjs'
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useProperties } from '@/api/queries/properties'
import { useDebts, useIncomeBook, useIncomeExpense, useOccupancy, useTaxBase, xlsxUrl } from '@/api/queries/reports'
import DateInput from '@/components/DateInput.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import { formatDate, formatMonth } from '@/utils/date'
import { contractStatusLabels, incomeKindLabels, propertyStatusLabels } from '@/utils/labels'
import { formatPct } from '@/utils/money'
import { parseProblem } from '@/utils/problem'
import { todayIso } from '@/utils/today'
import PeriodPicker from '../components/PeriodPicker.vue'
import { presetPeriod, type Period } from '../period'
import { reports, type ReportName } from '../reports'

const route = useRoute()
const name = computed(() => route.params.name as ReportName)
const meta = computed(() => reports.find((r) => r.name === name.value))
const today = todayIso()

// Параметры формы и «построенные» параметры (запрос — только по кнопке «Построить»).
const period = ref<Period>(presetPeriod('month', today))
const propertyId = ref<string | null>(null)
const asOf = ref<string | null>(today)
const year = ref<number>(dayjs(todayIso()).year())
const built = ref<Record<string, string | number> | null>(null)
watch(name, () => {
  built.value = null
  period.value = presetPeriod(name.value === 'occupancy' ? 'month' : 'year', today)
})

function build() {
  switch (meta.value?.params) {
    case 'period':
      built.value = {
        from: period.value.from,
        to: period.value.to,
        ...(propertyId.value ? { propertyId: propertyId.value } : {}),
      }
      break
    case 'asOf':
      built.value = { asOf: asOf.value ?? today }
      break
    case 'year':
      built.value = { year: year.value }
      break
  }
}

const p = (n: ReportName) => () => (name.value === n && built.value ? (built.value as never) : null)
const incomeExpense = useIncomeExpense(p('income-expense'))
const occupancy = useOccupancy(p('occupancy'))
const debts = useDebts(p('debts'))
const taxBase = useTaxBase(p('tax-base'))
const incomeBook = useIncomeBook(p('income-book'))
const active = computed(
  () =>
    ({
      'income-expense': incomeExpense,
      occupancy,
      debts,
      'tax-base': taxBase,
      'income-book': incomeBook,
    })[name.value],
)
const exportUrl = computed(() => (built.value ? xlsxUrl(`/reports/${name.value}.xlsx`, built.value) : null))

const properties = useProperties(() => ({ pageSize: 100 }))
const propertyOptions = computed(() =>
  (properties.data.value?.items ?? []).map((x) => ({ value: x.id, label: x.code })),
)
const years = computed(() => Array.from({ length: 6 }, (_, i) => dayjs(todayIso()).year() - i))
const taxTab = ref('summary')
</script>

<template>
  <div>
    <RouterLink to="/reports" class="back"><i class="pi pi-arrow-left" aria-hidden="true" /> Отчёты</RouterLink>
    <PageHeader :title="meta?.title ?? 'Отчёт'">
      <template #actions>
        <Button
          v-if="exportUrl"
          as="a"
          :href="exportUrl"
          download
          label="Выгрузить в Excel"
          icon="pi pi-file-excel"
          outlined
        />
      </template>
    </PageHeader>
    <Message v-if="!meta" severity="error" :closable="false">Отчёт не найден</Message>
    <template v-else>
      <form class="params" @submit.prevent="build">
        <PeriodPicker v-if="meta.params === 'period'" v-model="period" />
        <Select
          v-if="name === 'income-expense'"
          v-model="propertyId"
          :options="propertyOptions"
          option-label="label"
          option-value="value"
          placeholder="Все объекты"
          aria-label="Объект"
          show-clear
          filter
        />
        <div v-if="meta.params === 'asOf'" class="date">
          <label for="rp-asof" class="sr-only">На дату</label>
          <DateInput v-model="asOf" input-id="rp-asof" placeholder="На дату" />
        </div>
        <Select v-if="meta.params === 'year'" v-model="year" :options="years" aria-label="Год" />
        <Button type="submit" label="Построить" icon="pi pi-play" :loading="active?.isFetching.value" />
      </form>
      <Message v-if="active?.isError.value" severity="error" :closable="false">
        {{ parseProblem(active.error.value).message }}
      </Message>
      <p v-if="!built" class="hint">Задайте параметры и нажмите «Построить».</p>

      <!-- Доходы и расходы по объектам -->
      <DataTable
        v-if="name === 'income-expense' && incomeExpense.data.value"
        :value="incomeExpense.data.value.rows"
        size="small"
        scrollable
        striped-rows
      >
        <Column header="Объект" field="propertyCode" frozen />
        <Column header="Начислено"
          ><template #body="{ data }"><MoneyText :kop="data.chargedKop" /></template
        ></Column>
        <Column header="Плата за наём"
          ><template #body="{ data }"><MoneyText :kop="data.rentKop" /></template
        ></Column>
        <Column header="ЖКУ"
          ><template #body="{ data }"><MoneyText :kop="data.utilitiesKop" /></template
        ></Column>
        <Column header="Пени"
          ><template #body="{ data }"><MoneyText :kop="data.penaltyKop" /></template
        ></Column>
        <Column header="Ущерб"
          ><template #body="{ data }"><MoneyText :kop="data.damageKop" /></template
        ></Column>
        <Column header="Прочее"
          ><template #body="{ data }"><MoneyText :kop="data.otherKop" /></template
        ></Column>
        <Column header="Авансы"
          ><template #body="{ data }"><MoneyText :kop="data.advanceKop" /></template
        ></Column>
        <Column header="Удержания"
          ><template #body="{ data }"><MoneyText :kop="data.depositWithheldKop" /></template
        ></Column>
        <Column header="Возвраты"
          ><template #body="{ data }"><MoneyText :kop="data.refundsKop" /></template
        ></Column>
        <Column header="Перенос в залог"
          ><template #body="{ data }"><MoneyText :kop="data.toDepositKop" /></template
        ></Column>
        <Column header="Поступления"
          ><template #body="{ data }"><MoneyText :kop="data.incomeKop" /></template
        ></Column>
        <Column header="Расходы"
          ><template #body="{ data }"><MoneyText :kop="data.expensesKop" /></template
        ></Column>
        <Column header="Результат"
          ><template #body="{ data }"><MoneyText :kop="data.resultKop" /></template
        ></Column>
        <Column header="Рентаб."
          ><template #body="{ data }">{{ formatPct(data.marginPct) }}</template></Column
        >
        <Column header="Заполн."
          ><template #body="{ data }">{{ formatPct(data.occupancyPct) }}</template></Column
        >
        <ColumnGroup type="footer">
          <Row>
            <Column footer="Итого" />
            <Column
              v-for="f in [
                'chargedKop',
                'rentKop',
                'utilitiesKop',
                'penaltyKop',
                'damageKop',
                'otherKop',
                'advanceKop',
                'depositWithheldKop',
                'refundsKop',
                'toDepositKop',
                'incomeKop',
                'expensesKop',
                'resultKop',
              ]"
              :key="f"
            >
              <template #footer>
                <MoneyText :kop="(incomeExpense.data.value.totals as unknown as Record<string, number>)[f]" />
              </template>
            </Column>
            <Column :footer="formatPct(incomeExpense.data.value.totals.marginPct)" />
            <Column :footer="formatPct(incomeExpense.data.value.totals.occupancyPct)" />
          </Row>
        </ColumnGroup>
      </DataTable>

      <!-- Заполняемость и простои -->
      <template v-if="name === 'occupancy' && occupancy.data.value">
        <DataTable :value="occupancy.data.value.rows" size="small" striped-rows>
          <Column header="Объект" field="propertyCode" />
          <Column header="Дней" field="periodDays" />
          <Column header="Сдан" field="rentedDays" />
          <Column header="Свободен" field="freeDays" />
          <Column header="Забронирован" field="reservedDays" />
          <Column header="На ремонте" field="repairDays" />
          <Column header="Заполняемость"
            ><template #body="{ data }">{{ formatPct(data.occupancyPct) }}</template></Column
          >
          <Column header="Ожидаемая плата"
            ><template #body="{ data }"><MoneyText :kop="data.expectedRentKop" /></template
          ></Column>
          <Column header="Недополучено"
            ><template #body="{ data }"><MoneyText :kop="data.lostIncomeKop" /></template
          ></Column>
        </DataTable>
        <p class="total">
          Итого: заполняемость {{ formatPct(occupancy.data.value.totals.occupancyPct) }}, недополученный доход
          <MoneyText :kop="occupancy.data.value.totals.lostIncomeKop" />
        </p>
        <h3>Периоды простоя</h3>
        <DataTable :value="occupancy.data.value.idlePeriods" size="small" striped-rows>
          <template #empty>Простоев нет</template>
          <Column header="Объект" field="propertyCode" />
          <Column header="Статус"
            ><template #body="{ data }">{{ propertyStatusLabels[data.status as 'free'] }}</template></Column
          >
          <Column header="С"
            ><template #body="{ data }">{{ formatDate(data.from) }}</template></Column
          >
          <Column header="По"
            ><template #body="{ data }">{{ formatDate(data.to) }}</template></Column
          >
          <Column header="Дней" field="days" />
          <Column header="Недополучено"
            ><template #body="{ data }"><MoneyText :kop="data.lostIncomeKop" /></template
          ></Column>
        </DataTable>
      </template>

      <!-- Задолженность -->
      <template v-if="name === 'debts' && debts.data.value">
        <DataTable :value="debts.data.value.rows" size="small" scrollable striped-rows>
          <template #empty>Задолженности нет</template>
          <Column header="Наниматель" frozen>
            <template #body="{ data }">
              <RouterLink :to="`/tenants/${data.tenant.id}`">{{ data.tenant.displayName }}</RouterLink>
            </template>
          </Column>
          <Column header="Договор">
            <template #body="{ data }">
              <RouterLink :to="`/contracts/${data.contract.id}`">№ {{ data.contract.number }}</RouterLink>
              <div class="hint">{{ contractStatusLabels[data.contractStatus as 'active'] }}</div>
            </template>
          </Column>
          <Column header="Объект"
            ><template #body="{ data }">{{ data.property.code }}</template></Column
          >
          <Column header="Срок не наступил"
            ><template #body="{ data }"><MoneyText :kop="data.notDueKop" /></template
          ></Column>
          <Column header="1–30 дн."
            ><template #body="{ data }"><MoneyText :kop="data.days1to30Kop" /></template
          ></Column>
          <Column header="31–60 дн."
            ><template #body="{ data }"><MoneyText :kop="data.days31to60Kop" /></template
          ></Column>
          <Column header="61–90 дн."
            ><template #body="{ data }"><MoneyText :kop="data.days61to90Kop" /></template
          ></Column>
          <Column header="Более 90 дн."
            ><template #body="{ data }"><MoneyText :kop="data.over90Kop" /></template
          ></Column>
          <Column header="Пени"
            ><template #body="{ data }"><MoneyText :kop="data.penaltiesKop" /></template
          ></Column>
          <Column header="Итого долг"
            ><template #body="{ data }"
              ><b><MoneyText :kop="data.totalDebtKop" /></b></template
          ></Column>
          <Column header="Залог"
            ><template #body="{ data }"><MoneyText :kop="data.depositBalanceKop" /></template
          ></Column>
          <Column header="Аванс"
            ><template #body="{ data }"><MoneyText :kop="data.advanceKop" /></template
          ></Column>
          <Column header="Последний платёж"
            ><template #body="{ data }">{{ formatDate(data.lastPaymentOn) || '—' }}</template></Column
          >
        </DataTable>
        <p class="total">
          Итого долг: <MoneyText :kop="debts.data.value.totals.totalDebtKop" /> · случаев просрочки за
          {{ formatDate(debts.data.value.overdue.from) }} – {{ formatDate(debts.data.value.overdue.to) }}:
          {{ debts.data.value.overdue.count }}, в среднем
          {{ (debts.data.value.overdue.avgDays ?? '—').replace('.', ',') }} дн.
        </p>
      </template>

      <!-- Налоговая база -->
      <template v-if="name === 'tax-base' && taxBase.data.value">
        <p class="hint">
          {{ taxBase.data.value.regimeTitle }} · справочно: внесено залогов
          <MoneyText :kop="taxBase.data.value.depositsReceivedKop" />, возвращено
          <MoneyText :kop="taxBase.data.value.depositsRefundedKop" />
        </p>
        <Message v-if="taxBase.data.value.note" severity="info" :closable="false">{{
          taxBase.data.value.note
        }}</Message>
        <Tabs v-model:value="taxTab">
          <TabList>
            <Tab value="summary">Сводка</Tab>
            <Tab value="operations">Операции</Tab>
          </TabList>
          <TabPanels>
            <TabPanel value="summary">
              <DataTable
                :value="taxBase.data.value.periods"
                size="small"
                scrollable
                :row-class="(r) => (r.cumulative ? 'cumulative' : '')"
              >
                <Column header="Период" frozen>
                  <template #body="{ data }">{{
                    data.cumulative ? `Нарастающим итогом: ${data.label}` : data.label
                  }}</template>
                </Column>
                <Column header="Плата за наём"
                  ><template #body="{ data }"><MoneyText :kop="data.rentKop" /></template
                ></Column>
                <Column header="ЖКУ"
                  ><template #body="{ data }"><MoneyText :kop="data.utilitiesKop" /></template
                ></Column>
                <Column header="Пени"
                  ><template #body="{ data }"><MoneyText :kop="data.penaltyKop" /></template
                ></Column>
                <Column header="Ущерб"
                  ><template #body="{ data }"><MoneyText :kop="data.damageKop" /></template
                ></Column>
                <Column header="Прочее"
                  ><template #body="{ data }"><MoneyText :kop="data.otherKop" /></template
                ></Column>
                <Column header="Авансы"
                  ><template #body="{ data }"><MoneyText :kop="data.advanceKop" /></template
                ></Column>
                <Column header="Удержания"
                  ><template #body="{ data }"><MoneyText :kop="data.depositWithheldKop" /></template
                ></Column>
                <Column header="Возвраты"
                  ><template #body="{ data }"><MoneyText :kop="data.refundsKop" /></template
                ></Column>
                <Column header="Перенос в залог"
                  ><template #body="{ data }"><MoneyText :kop="data.toDepositKop" /></template
                ></Column>
                <Column header="Доходы"
                  ><template #body="{ data }"
                    ><b><MoneyText :kop="data.incomeKop" /></b></template
                ></Column>
                <Column v-if="taxBase.data.value.taxRegime === 'usn_income_expense'" header="Расходы">
                  <template #body="{ data }"><MoneyText :kop="data.expensesKop ?? 0" /></template>
                </Column>
                <Column header="База"
                  ><template #body="{ data }"><MoneyText :kop="data.taxBaseKop" /></template
                ></Column>
                <Column v-if="taxBase.data.value.taxRegime === 'npd'" header="Налог 4 % / 6 %">
                  <template #body="{ data }">
                    <MoneyText :kop="data.taxIndividualsKop ?? 0" /> / <MoneyText :kop="data.taxLegalKop ?? 0" />
                  </template>
                </Column>
                <Column v-else-if="!taxBase.data.value.note" header="Налог">
                  <template #body="{ data }"
                    ><b><MoneyText :kop="data.taxKop ?? 0" /></b
                  ></template>
                </Column>
              </DataTable>
            </TabPanel>
            <TabPanel value="operations">
              <DataTable :value="taxBase.data.value.operations" size="small" paginator :rows="50" striped-rows>
                <template #empty>Операций нет</template>
                <Column header="Дата"
                  ><template #body="{ data }">{{ formatDate(data.date) }}</template></Column
                >
                <Column header="Документ" field="document" />
                <Column header="Договор"
                  ><template #body="{ data }">№ {{ data.contract.number }}</template></Column
                >
                <Column header="Наниматель"
                  ><template #body="{ data }">{{ data.tenant.displayName }}</template></Column
                >
                <Column header="Объект"
                  ><template #body="{ data }">{{ data.property.code }}</template></Column
                >
                <Column header="Вид"
                  ><template #body="{ data }">{{ incomeKindLabels[data.kind as 'rent'] }}</template></Column
                >
                <Column header="Сумма"
                  ><template #body="{ data }"><MoneyText :kop="data.amountKop" /></template
                ></Column>
              </DataTable>
            </TabPanel>
          </TabPanels>
        </Tabs>
      </template>

      <!-- Доходы для книги учёта -->
      <template v-if="name === 'income-book' && incomeBook.data.value">
        <p class="hint">
          {{ incomeBook.data.value.orgName }}, ИНН {{ incomeBook.data.value.inn }} · объект налогообложения:
          {{ incomeBook.data.value.taxObject }}
        </p>
        <DataTable
          :value="incomeBook.data.value.rows"
          size="small"
          :row-class="(r) => (r.rowType !== 'operation' ? 'cumulative' : '')"
        >
          <Column header="№" field="no" />
          <Column header="Дата и номер документа"
            ><template #body="{ data }">{{ data.document ?? '' }}</template></Column
          >
          <Column header="Содержание операции" field="description" />
          <Column header="Доходы"
            ><template #body="{ data }"><MoneyText :kop="data.incomeKop" /></template
          ></Column>
          <Column v-if="incomeBook.data.value.rows.some((r) => r.expensesKop !== null)" header="Расходы">
            <template #body="{ data }"><MoneyText v-if="data.expensesKop !== null" :kop="data.expensesKop" /></template>
          </Column>
        </DataTable>
      </template>
      <p v-if="name === 'income-expense' && incomeExpense.data.value?.byMonth.length" class="hint">
        Помесячная разбивка ({{ formatMonth(incomeExpense.data.value.byMonth[0]!.month) }} и далее) — на листе «По
        месяцам» в выгрузке Excel.
      </p>
    </template>
  </div>
</template>

<style scoped>
.back {
  display: inline-block;
  margin-bottom: 0.5rem;
}
.params {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1rem;
}
.date {
  width: 11rem;
}
.hint {
  color: var(--p-text-muted-color);
}
.total {
  margin: 0.5rem 0 1rem;
  font-weight: 600;
}
:deep(.cumulative) {
  font-weight: 600;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
}
</style>
