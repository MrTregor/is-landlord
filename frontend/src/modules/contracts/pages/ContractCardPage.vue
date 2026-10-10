<script setup lang="ts">
// Карточка договора (14 Экраны собственника, раздел 4.3).
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { useContract, useContractMutations } from '@/api/queries/contracts'
import { useTenantMutations } from '@/api/queries/tenants'
import MoneyText from '@/components/MoneyText.vue'
import PageHeader from '@/components/PageHeader.vue'
import PlannedTab from '@/components/PlannedTab.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate } from '@/utils/date'
import { contractStatusLabels, contractStatusSeverity, legalBasisLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import ActivateDialog from '../components/ActivateDialog.vue'
import ActsTab from '../components/ActsTab.vue'
import AmendmentsTab from '../components/AmendmentsTab.vue'
import ChargesTab from '../components/ChargesTab.vue'
import TerminateDialog from '../components/TerminateDialog.vue'
import TermsTab from '../components/TermsTab.vue'
import TimelineTab from '../components/TimelineTab.vue'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const confirm = useConfirm()
const id = computed(() => route.params.id as string)
const contract = useContract(id)
const m = useContractMutations(id)
const c = computed(() => contract.data.value)
const tenantM = useTenantMutations(() => c.value?.tenant.id ?? '')

const live = computed(() => c.value?.status === 'active' || c.value?.status === 'extended')
const closed = computed(() => c.value?.status === 'terminated' || c.value?.status === 'finished')

const tabs = [
  { value: 'terms', label: 'Условия' },
  { value: 'charges', label: 'Начисления' },
  { value: 'payments', label: 'Платежи', stage: 'M6' },
  { value: 'deposit', label: 'Залог', stage: 'M6' },
  { value: 'documents', label: 'Документы', stage: 'M5' },
  { value: 'amendments', label: 'Допсоглашения' },
  { value: 'acts', label: 'Акты' },
  { value: 'timeline', label: 'Журнал' },
]
const tab = computed({
  get: () =>
    typeof route.query.tab === 'string' && tabs.some((t) => t.value === route.query.tab) ? route.query.tab : 'terms',
  set: (v: string | number) =>
    void router.replace({ query: { ...route.query, tab: v === 'terms' ? undefined : String(v) } }),
})

function fail(err: unknown) {
  toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
}

const activateDialog = ref(false)
const terminateDialog = ref(false)
const amendmentSignal = ref(0)
const returnSignal = ref(0)

function newAmendment() {
  tab.value = 'amendments'
  amendmentSignal.value++
}
function returnAct() {
  tab.value = 'acts'
  returnSignal.value++
}

function deleteDraft() {
  confirm.require({
    header: 'Удалить черновик?',
    message: `Черновик договора № ${c.value?.number} будет удалён вместе с условиями и проживающими. Номер не возвращается.`,
    acceptProps: { label: 'Удалить', severity: 'danger' },
    rejectProps: { label: 'Отмена', outlined: true },
    accept: async () => {
      try {
        await m.remove.mutateAsync()
        toast.add({ severity: 'success', summary: 'Черновик удалён', life: 3000 })
        await router.push('/contracts')
      } catch (err) {
        fail(err)
      }
    },
  })
}

async function inviteTenant() {
  try {
    await tenantM.invite.mutateAsync()
    toast.add({ severity: 'success', summary: 'Приглашение отправлено', life: 3000 })
  } catch (err) {
    fail(err)
  }
}

const moreMenu = ref()
const moreItems = computed(() => [
  { label: 'Сформировать договор заново', icon: 'pi pi-file-pdf', disabled: true },
  { label: 'Акт сверки', icon: 'pi pi-list', disabled: true },
  { label: 'Пригласить в кабинет', icon: 'pi pi-send', disabled: !c.value?.tenant.email, command: inviteTenant },
  { label: 'Взаиморасчёт', icon: 'pi pi-calculator', command: () => router.push(`/contracts/${id.value}/settlement`) },
])
</script>

<template>
  <div>
    <Skeleton v-if="contract.isPending.value" height="12rem" />
    <Message v-else-if="contract.isError.value" severity="error" :closable="false">
      {{ parseProblem(contract.error.value).message }}
    </Message>
    <template v-else-if="c">
      <PageHeader :title="`${legalBasisLabels[c.legalBasis]} № ${c.number}`">
        <template #actions>
          <template v-if="c.status === 'draft'">
            <Button label="Изменить" icon="pi pi-pencil" outlined @click="router.push(`/contracts/${c.id}/edit`)" />
            <Button
              label="Сформировать проект"
              icon="pi pi-file-pdf"
              outlined
              disabled
              title="Документы формируются с этапа M5"
            />
            <Button label="Ввести в действие" icon="pi pi-check" @click="activateDialog = true" />
            <Button label="Удалить черновик" icon="pi pi-trash" severity="danger" outlined @click="deleteDraft" />
          </template>
          <template v-else-if="live">
            <Button label="Зарегистрировать платёж" icon="pi pi-wallet" outlined disabled title="Платежи — этап M6" />
            <Button label="Допсоглашение" icon="pi pi-file-edit" outlined @click="newAmendment" />
            <Button label="Акт возврата" icon="pi pi-replay" outlined @click="returnAct" />
            <Button label="Расторгнуть" icon="pi pi-times" severity="danger" outlined @click="terminateDialog = true" />
            <Button label="Ещё" icon="pi pi-ellipsis-v" text @click="(e) => moreMenu?.toggle(e)" />
            <Menu ref="moreMenu" :model="moreItems" popup />
          </template>
          <template v-else-if="closed">
            <Button label="Зарегистрировать платёж" icon="pi pi-wallet" outlined disabled title="Платежи — этап M6" />
            <Button
              label="Взаиморасчёт"
              icon="pi pi-calculator"
              outlined
              @click="router.push(`/contracts/${c.id}/settlement`)"
            />
            <Button label="Акт возврата" icon="pi pi-replay" outlined @click="returnAct" />
            <Button label="Акт сверки" icon="pi pi-list" outlined disabled title="Акт сверки — этап M6" />
          </template>
        </template>
      </PageHeader>

      <div class="headline">
        <StatusTag :label="contractStatusLabels[c.status]" :severity="contractStatusSeverity[c.status]" />
        <Tag v-if="c.isShortTerm" value="Краткосрочный" severity="secondary" />
        <span>
          Объект: <RouterLink :to="`/properties/${c.property.id}`">{{ c.property.code }}</RouterLink
          >, {{ c.property.address }}
        </span>
        <span>
          {{ c.legalBasis === 'ch34' ? 'Арендатор' : 'Наниматель' }}:
          <RouterLink :to="`/tenants/${c.tenant.id}`">{{ c.tenant.displayName }}</RouterLink>
        </span>
        <span>Срок: {{ formatDate(c.startDate) }} – {{ formatDate(c.actualEndDate) }}</span>
      </div>

      <div class="kpis">
        <div class="kpi">
          <div class="kpi__label">Долг</div>
          <div class="kpi__value"><MoneyText :kop="c.balances.debtKop" /></div>
          <div class="hint">просрочено <MoneyText :kop="c.balances.overdueKop" /></div>
        </div>
        <div class="kpi">
          <div class="kpi__label">Аванс</div>
          <div class="kpi__value"><MoneyText :kop="c.balances.advanceKop" /></div>
        </div>
        <div class="kpi">
          <div class="kpi__label">Пени начисляются</div>
          <div class="kpi__value"><MoneyText :kop="c.balances.penaltiesAccruingKop" /></div>
        </div>
        <div class="kpi">
          <div class="kpi__label">Залог</div>
          <div class="kpi__value">
            внесено <MoneyText :kop="c.balances.depositReceivedKop" /> из
            <MoneyText :kop="c.balances.depositExpectedKop" />
          </div>
          <div class="hint">остаток <MoneyText :kop="c.balances.depositBalanceKop" /></div>
        </div>
        <div class="kpi">
          <div class="kpi__label">Следующий платёж</div>
          <div v-if="c.nextDue" class="kpi__value">
            {{ formatDate(c.nextDue.date) }} · <MoneyText :kop="c.nextDue.amountKop" />
          </div>
          <div v-else class="kpi__value">—</div>
        </div>
      </div>

      <Message v-for="w in c.warnings" :key="w" severity="warn" :closable="false" class="warn">{{ w }}</Message>
      <Message
        v-if="c.status === 'draft' && c.activationBlockers.length"
        severity="error"
        :closable="false"
        class="warn"
      >
        Ввести в действие пока нельзя: {{ c.activationBlockers.join('; ') }}
      </Message>

      <Tabs v-model:value="tab" scrollable>
        <TabList>
          <Tab v-for="t in tabs" :key="t.value" :value="t.value">{{ t.label }}</Tab>
        </TabList>
        <TabPanels>
          <TabPanel value="terms"><TermsTab :contract="c" /></TabPanel>
          <TabPanel value="charges"><ChargesTab v-if="tab === 'charges'" :contract="c" /></TabPanel>
          <TabPanel value="amendments">
            <AmendmentsTab v-if="tab === 'amendments'" :contract="c" :create-signal="amendmentSignal" />
          </TabPanel>
          <TabPanel value="acts"
            ><ActsTab v-if="tab === 'acts'" :contract="c" :return-signal="returnSignal"
          /></TabPanel>
          <TabPanel value="timeline"><TimelineTab v-if="tab === 'timeline'" :contract-id="c.id" /></TabPanel>
          <template v-for="t in tabs" :key="t.value">
            <TabPanel v-if="t.stage" :value="t.value"><PlannedTab :stage="t.stage" /></TabPanel>
          </template>
        </TabPanels>
      </Tabs>

      <ActivateDialog v-model:visible="activateDialog" :contract="c" />
      <TerminateDialog v-if="live" v-model:visible="terminateDialog" :contract="c" />
    </template>
  </div>
</template>

<style scoped>
.headline {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem 1.25rem;
  margin: -0.5rem 0 1rem;
}
.kpis {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 0.75rem;
  margin-bottom: 1rem;
}
.kpi {
  background: var(--p-content-background);
  border: 1px solid var(--p-content-border-color);
  border-radius: 6px;
  padding: 0.75rem 1rem;
}
.kpi__label {
  color: var(--p-text-muted-color);
  font-size: 0.85rem;
}
.kpi__value {
  font-weight: 600;
  margin-top: 0.25rem;
}
.warn {
  margin-bottom: 0.75rem;
}
</style>
