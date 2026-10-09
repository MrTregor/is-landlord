<script setup lang="ts">
// Карточка объекта (14 Экраны собственника, раздел 2.3).
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { useProperty, usePropertyMutations } from '@/api/queries/properties'
import DateInput from '@/components/DateInput.vue'
import PageHeader from '@/components/PageHeader.vue'
import PlannedTab from '@/components/PlannedTab.vue'
import StatusTag from '@/components/StatusTag.vue'
import { formatDate, toIsoDate } from '@/utils/date'
import { propertyKindLabels, propertyStatusLabels, propertyStatusSeverity } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import ContractsTab from '../components/ContractsTab.vue'
import InventoryTab from '../components/InventoryTab.vue'
import MetersTab from '../components/MetersTab.vue'
import OverviewTab from '../components/OverviewTab.vue'
import StatusHistoryTab from '../components/StatusHistoryTab.vue'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const confirm = useConfirm()
const id = computed(() => route.params.id as string)
const property = useProperty(id)
const m = usePropertyMutations(id)
const p = computed(() => property.data.value)

const tabs = [
  { value: 'overview', label: 'Обзор' },
  { value: 'contracts', label: 'Договоры' },
  { value: 'charges', label: 'Начисления', stage: 'M4' },
  { value: 'expenses', label: 'Затраты', stage: 'M11' },
  { value: 'requests', label: 'Заявки', stage: 'M11' },
  { value: 'meters', label: 'Счётчики' },
  { value: 'inventory', label: 'Опись имущества' },
  { value: 'history', label: 'История статусов' },
  { value: 'profitability', label: 'Доходность', stage: 'M12' },
]
const tab = computed({
  get: () =>
    typeof route.query.tab === 'string' && tabs.some((t) => t.value === route.query.tab) ? route.query.tab : 'overview',
  set: (v: string | number) =>
    void router.replace({ query: { ...route.query, tab: v === 'overview' ? undefined : String(v) } }),
})

function fail(err: unknown) {
  toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(err).message, life: 6000 })
}

function toggleArchive() {
  const archive = !p.value?.archivedAt
  confirm.require({
    header: archive ? 'Перевести объект в архив?' : 'Вернуть объект из архива?',
    message: archive
      ? 'Архивный объект скрыт из списков и недоступен для новых договоров.'
      : 'Объект снова появится в списках.',
    acceptLabel: archive ? 'В архив' : 'Вернуть',
    rejectLabel: 'Отмена',
    accept: async () => {
      try {
        await m.archive.mutateAsync(archive)
        toast.add({ severity: 'success', summary: archive ? 'Объект в архиве' : 'Объект возвращён', life: 3000 })
      } catch (err) {
        fail(err)
      }
    },
  })
}

// Диалог «Сменить статус» (05 разд. 2.1): «Сдан» вручную не ставится.
const statusDialog = ref(false)
const statusForm = reactive({
  status: 'free' as 'free' | 'reserved' | 'repair',
  since: toIsoDate(new Date()) as string | null,
  reason: '',
})
const statusError = ref<string | null>(null)
const manualStatuses = [
  { value: 'free', label: 'Свободен' },
  { value: 'reserved', label: 'Забронирован' },
  { value: 'repair', label: 'На ремонте' },
]
function openStatus() {
  statusForm.status = p.value?.status === 'free' ? 'repair' : 'free'
  statusForm.since = toIsoDate(new Date())
  statusForm.reason = ''
  statusError.value = null
  statusDialog.value = true
}
async function saveStatus() {
  if (!statusForm.since) {
    statusError.value = 'Укажите дату'
    return
  }
  try {
    await m.status.mutateAsync({
      status: statusForm.status,
      since: statusForm.since,
      reason: statusForm.reason.trim() || null,
    })
    statusDialog.value = false
    toast.add({ severity: 'success', summary: 'Статус изменён', life: 3000 })
  } catch (err) {
    statusError.value = parseProblem(err).message
  }
}
</script>

<template>
  <div>
    <Skeleton v-if="property.isPending.value" height="10rem" />
    <Message v-else-if="property.isError.value" severity="error" :closable="false">
      {{ parseProblem(property.error.value).message }}
    </Message>
    <template v-else-if="p">
      <PageHeader :title="p.code" :subtitle="`${propertyKindLabels[p.kind]} · ${p.address}`">
        <template #actions>
          <Button label="Изменить" icon="pi pi-pencil" outlined @click="router.push(`/properties/${p.id}/edit`)" />
          <Button
            label="Сменить статус"
            icon="pi pi-sync"
            outlined
            :disabled="!!p.archivedAt || p.status === 'rented'"
            :title="p.status === 'rented' ? 'Статус «Сдан» меняется только договором' : undefined"
            @click="openStatus"
          />
          <Button
            :label="p.archivedAt ? 'Вернуть из архива' : 'В архив'"
            icon="pi pi-box"
            outlined
            @click="toggleArchive"
          />
          <Button
            label="Новый договор"
            icon="pi pi-file-plus"
            :disabled="!!p.archivedAt"
            @click="router.push({ path: '/contracts/new', query: { propertyId: p.id } })"
          />
        </template>
      </PageHeader>
      <div class="status-line">
        <StatusTag
          :label="p.archivedAt ? 'В архиве' : propertyStatusLabels[p.status]"
          :severity="p.archivedAt ? 'secondary' : propertyStatusSeverity[p.status]"
        />
        <span class="hint">с {{ formatDate(p.statusSince) }}</span>
        <span v-if="p.currentContract">
          · договор
          <RouterLink :to="`/contracts/${p.currentContract.id}`">№ {{ p.currentContract.number }}</RouterLink
          >, {{ p.currentContract.tenantName }}, до {{ formatDate(p.currentContract.endDate) }}
        </span>
      </div>

      <Tabs v-model:value="tab" scrollable>
        <TabList>
          <Tab v-for="t in tabs" :key="t.value" :value="t.value">{{ t.label }}</Tab>
        </TabList>
        <TabPanels>
          <TabPanel value="overview"><OverviewTab :property="p" /></TabPanel>
          <TabPanel value="contracts"><ContractsTab v-if="tab === 'contracts'" :property-id="p.id" /></TabPanel>
          <TabPanel value="meters"
            ><MetersTab v-if="tab === 'meters'" :property-id="p.id" :archived="!!p.archivedAt"
          /></TabPanel>
          <TabPanel value="inventory"><InventoryTab v-if="tab === 'inventory'" :property-id="p.id" /></TabPanel>
          <TabPanel value="history"><StatusHistoryTab v-if="tab === 'history'" :property-id="p.id" /></TabPanel>
          <template v-for="t in tabs" :key="t.value">
            <TabPanel v-if="t.stage" :value="t.value"><PlannedTab :stage="t.stage" /></TabPanel>
          </template>
        </TabPanels>
      </Tabs>

      <Dialog
        v-model:visible="statusDialog"
        modal
        header="Сменить статус"
        :style="{ width: '480px' }"
        :breakpoints="{ '575px': '100vw' }"
      >
        <form class="form" novalidate @submit.prevent="saveStatus">
          <div class="field">
            <label for="st-status">Статус *</label>
            <Select
              v-model="statusForm.status"
              input-id="st-status"
              :options="manualStatuses.filter((s) => s.value !== p!.status)"
              option-label="label"
              option-value="value"
              fluid
            />
          </div>
          <div class="field">
            <label for="st-since">С даты *</label>
            <DateInput v-model="statusForm.since" input-id="st-since" :max-date="new Date()" />
          </div>
          <div class="field">
            <label for="st-reason">Причина</label>
            <InputText id="st-reason" v-model="statusForm.reason" fluid />
          </div>
          <Message v-if="statusError" severity="error" :closable="false">{{ statusError }}</Message>
          <div class="actions">
            <Button type="button" label="Отмена" outlined @click="statusDialog = false" />
            <Button type="submit" label="Сменить статус" :loading="m.status.isPending.value" />
          </div>
        </form>
      </Dialog>
    </template>
  </div>
</template>

<style scoped>
.status-line {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  margin: -0.75rem 0 1rem;
}
</style>
