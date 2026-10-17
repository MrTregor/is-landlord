<script setup lang="ts">
// Сводка (14 Экраны собственника, раздел 1): четыре показателя, график «Поступления по месяцам», таблица объектов,
// ближайшие события (7 дней и просрочки); пустое состояние без объектов.
import dayjs from 'dayjs'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useDashboard } from '@/api/queries/dashboard'
import EmptyState from '@/components/EmptyState.vue'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import { propertyStatusLabels, propertyStatusSeverity } from '@/utils/labels'
import { formatKop, formatPct } from '@/utils/money'
import { parseProblem } from '@/utils/problem'

const router = useRouter()
const dash = useDashboard()
const d = computed(() => dash.data.value)

const monthsNom = [
  'январь',
  'февраль',
  'март',
  'апрель',
  'май',
  'июнь',
  'июль',
  'август',
  'сентябрь',
  'октябрь',
  'ноябрь',
  'декабрь',
]
const monthsShort = ['янв', 'фев', 'мар', 'апр', 'май', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']
const monthName = computed(() => (d.value ? monthsNom[dayjs(d.value.today).month()] : ''))
const change = computed(() => {
  const x = d.value
  if (!x || !x.incomePrevMonthKop) return null
  const pct = ((x.incomeMonthKop - x.incomePrevMonthKop) * 100) / x.incomePrevMonthKop
  return `${pct >= 0 ? '+' : ''}${pct.toFixed(1).replace('.', ',')} % к прошлому месяцу`
})
const rented = computed(() => d.value?.properties.filter((p) => p.status === 'rented').length ?? 0)
const attention = computed(() => {
  const x = d.value
  if (!x) return 0
  return x.openDiscrepancies + x.openRequests + x.pendingReadings + x.fiscalProblems
})
const menu = ref()
const attentionItems = computed(() => {
  const x = d.value
  if (!x) return []
  return [
    { label: `Расхождения: ${x.openDiscrepancies}`, icon: 'pi pi-sync', command: () => router.push('/reconciliation') },
    {
      label: `Новые заявки: ${x.openRequests}`,
      icon: 'pi pi-wrench',
      command: () => router.push('/requests?status=new'),
    },
    {
      label: `Показания на проверке: ${x.pendingReadings}`,
      icon: 'pi pi-gauge',
      command: () => router.push('/readings'),
    },
    {
      label: `Ошибки чеков: ${x.fiscalProblems}`,
      icon: 'pi pi-receipt',
      command: () => router.push('/fiscal-receipts'),
    },
  ]
})

const chartData = computed(() => {
  const months = d.value?.incomeByMonth ?? []
  return {
    labels: months.map((m) => monthsShort[dayjs(m.month).month()]),
    datasets: [{ label: 'Поступления', data: months.map((m) => m.amountKop / 100), backgroundColor: '#10b981' }],
  }
})
const chartOptions = {
  maintainAspectRatio: false,
  plugins: {
    legend: { display: false },
    tooltip: { callbacks: { label: (c: { raw: unknown }) => formatKop(Math.round(Number(c.raw) * 100)) } },
  },
  scales: { y: { beginAtZero: true, ticks: { callback: (v: unknown) => Number(v).toLocaleString('ru-RU') } } },
}

const eventLink = (e: NonNullable<typeof d.value>['upcoming'][number]) =>
  e.contract ? `/contracts/${e.contract.id}` : e.property ? `/properties/${e.property.id}` : '/calendar'
</script>

<template>
  <div>
    <PageHeader title="Сводка" />
    <Skeleton v-if="dash.isPending.value" height="20rem" />
    <Message v-else-if="dash.isError.value" severity="error" :closable="false">
      {{ parseProblem(dash.error.value).message }}
    </Message>
    <EmptyState
      v-else-if="d && !d.properties.length"
      text="Начните с добавления объектов недвижимости"
      icon="pi pi-building"
      action-label="Добавить объект"
      @action="router.push('/properties')"
    />
    <template v-else-if="d">
      <div class="kpis">
        <div class="kpi">
          <span class="kpi__label">Поступления за {{ monthName }}</span>
          <span class="kpi__value"><MoneyText :kop="d.incomeMonthKop" /></span>
          <span class="kpi__hint">{{ change ?? '—' }}</span>
        </div>
        <RouterLink class="kpi" to="/reports/debts">
          <span class="kpi__label">Задолженность</span>
          <span class="kpi__value"><MoneyText :kop="d.debtTotalKop" /></span>
          <span class="kpi__hint"
            >в т. ч. просрочено {{ formatKop(d.overdueKop) }} ({{ d.overdueContracts }} дог.)</span
          >
        </RouterLink>
        <RouterLink class="kpi" to="/reports/occupancy">
          <span class="kpi__label">Заполняемость</span>
          <span class="kpi__value">{{ formatPct(d.occupancyPct) }}</span>
          <span class="kpi__hint">сдано {{ rented }} из {{ d.properties.length }}</span>
        </RouterLink>
        <button type="button" class="kpi kpi--button" aria-haspopup="true" @click="(e) => menu.toggle(e)">
          <span class="kpi__label">Требуют внимания</span>
          <span class="kpi__value" :class="{ warn: attention > 0 }">{{ attention }}</span>
          <span class="kpi__hint">расхождения, заявки, показания, чеки</span>
        </button>
        <Menu ref="menu" :model="attentionItems" popup />
      </div>

      <div class="grid">
        <section class="block">
          <h3>Поступления по месяцам</h3>
          <div class="chart">
            <Chart type="bar" :data="chartData" :options="chartOptions" />
          </div>
        </section>
        <section class="block">
          <div class="block__head">
            <h3>Ближайшие события</h3>
            <Button label="Открыть календарь" size="small" text @click="router.push('/calendar')" />
          </div>
          <p v-if="!d.upcoming.length" class="hint">На ближайшие 7 дней событий нет</p>
          <ul v-else class="events">
            <li v-for="(e, i) in d.upcoming" :key="e.id ?? i" :class="{ overdue: e.kind === 'overdue' }">
              <span class="events__date">{{ formatDate(e.date) }}</span>
              <RouterLink :to="eventLink(e)">{{ e.title }}</RouterLink>
              <MoneyText v-if="e.amountKop" :kop="e.amountKop" class="events__sum" />
            </li>
          </ul>
        </section>
      </div>

      <section class="block">
        <h3>Объекты</h3>
        <DataTable
          :value="d.properties"
          size="small"
          row-hover
          class="clickable"
          data-key="id"
          @row-click="(ev) => router.push(`/properties/${ev.data.id}`)"
        >
          <Column header="Объект">
            <template #body="{ data }">
              <RouterLink :to="`/properties/${data.id}`" @click.stop>{{ data.code }}</RouterLink>
            </template>
          </Column>
          <Column header="Статус">
            <template #body="{ data }">
              <StatusTag
                :label="propertyStatusLabels[data.status as 'free']"
                :severity="propertyStatusSeverity[data.status as 'free']"
              />
            </template>
          </Column>
          <Column header="Наниматель"
            ><template #body="{ data }">{{ data.tenantName ?? '—' }}</template></Column
          >
          <Column header="Договор">
            <template #body="{ data }">
              <template v-if="data.contractId">
                № {{ data.contractNumber }} <span class="hint">до {{ formatDate(data.endDate) }}</span>
              </template>
              <span v-else>—</span>
            </template>
          </Column>
          <Column header="Долг">
            <template #body="{ data }">
              <MoneyText v-if="data.debtKop" :kop="data.debtKop" :class="{ warn: data.debtKop > 0 }" />
              <span v-else>—</span>
            </template>
          </Column>
        </DataTable>
      </section>
    </template>
  </div>
</template>

<style scoped>
.kpis {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 1rem;
  margin-bottom: 1rem;
}
@media (max-width: 900px) {
  .kpis {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
.kpi {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 1rem;
  border: 1px solid var(--p-content-border-color);
  border-radius: var(--p-content-border-radius);
  background: var(--p-content-background);
  color: inherit;
  text-decoration: none;
  text-align: left;
  font: inherit;
}
.kpi--button {
  cursor: pointer;
}
a.kpi:hover,
.kpi--button:hover {
  border-color: var(--p-primary-color);
}
.kpi__label {
  color: var(--p-text-muted-color);
  font-size: 0.9rem;
}
.kpi__value {
  font-size: 1.5rem;
  font-weight: 600;
}
.kpi__hint {
  color: var(--p-text-muted-color);
  font-size: 0.8rem;
}
.warn {
  color: var(--p-red-600);
}
.grid {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  gap: 1rem;
}
@media (max-width: 900px) {
  .grid {
    grid-template-columns: 1fr;
  }
}
.block {
  margin-bottom: 1rem;
}
.block h3 {
  margin: 0 0 0.5rem;
  font-size: 1rem;
}
.block__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.block__head h3 {
  margin: 0;
}
.chart {
  height: 260px;
}
.events {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.events li {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  align-items: baseline;
}
.events li.overdue a {
  color: var(--p-red-600);
}
.events__date {
  color: var(--p-text-muted-color);
  min-width: 5.5rem;
}
.events__sum {
  margin-left: auto;
}
.hint {
  color: var(--p-text-muted-color);
}
.clickable :deep(tbody tr) {
  cursor: pointer;
}
</style>
