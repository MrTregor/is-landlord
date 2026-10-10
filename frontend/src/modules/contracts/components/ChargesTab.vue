<script setup lang="ts">
// Вкладка «Начисления» договора (14 Экраны собственника, раздел 4.3, вкладка 2).
import dayjs from 'dayjs'
import { computed, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useChargeMutations, type ChargesQuery } from '@/api/queries/charges'
import type { ContractDetail } from '@/api/queries/contracts'
import { formatKop } from '@/utils/money'
import { parseProblem } from '@/utils/problem'
import ChargeDrawer from '@/modules/charges/components/ChargeDrawer.vue'
import ChargesTable from '@/modules/charges/components/ChargesTable.vue'
import ManualChargeDialog from '@/modules/charges/components/ManualChargeDialog.vue'

const props = defineProps<{ contract: ContractDetail }>()
const toast = useToast()
const m = useChargeMutations()
const page = ref(1)
const pageSize = ref(25)
const sort = ref<ChargesQuery['sort']>()
const query = computed<ChargesQuery>(() => ({
  contractId: props.contract.id,
  page: page.value,
  pageSize: pageSize.value,
  sort: sort.value,
}))
const opened = ref<string | null>(null)
const manualDialog = ref(false)
const live = computed(() => props.contract.status !== 'draft')

// «Начисление ЖКУ за месяц» (режим «По счётчикам»).
const utilDialog = ref(false)
const month = ref<Date | null>(dayjs().subtract(1, 'month').startOf('month').toDate())
const utilError = ref<string | null>(null)
async function createUtilities() {
  utilError.value = null
  if (!month.value) {
    utilError.value = 'Выберите месяц'
    return
  }
  try {
    const ch = await m.utilities.mutateAsync({
      contractId: props.contract.id,
      periodMonth: dayjs(month.value).startOf('month').format('YYYY-MM-DD'),
    })
    utilDialog.value = false
    const warnings = (ch.calcDetails.warnings as string[] | undefined) ?? []
    toast.add({
      severity: warnings.length ? 'warn' : 'success',
      summary: `Сформировано начисление ЖКУ на ${formatKop(ch.amountKop)}`,
      detail: warnings.join('. ') || undefined,
      life: warnings.length ? 8000 : 3000,
    })
  } catch (err) {
    utilError.value = parseProblem(err).message
  }
}
</script>

<template>
  <div>
    <div v-if="live" class="actions actions--start toolbar">
      <Button label="Ручное начисление" icon="pi pi-plus" outlined @click="manualDialog = true" />
      <Button
        v-if="contract.utilitiesMode === 'meters'"
        label="Начисление ЖКУ за месяц"
        icon="pi pi-bolt"
        outlined
        @click="utilDialog = true"
      />
    </div>
    <ChargesTable
      :query="query"
      @page="
        (p, s) => {
          page = p
          pageSize = s
        }
      "
      @sort="(s) => (sort = s as ChargesQuery['sort'])"
      @open="(id) => (opened = id)"
    />
    <ChargeDrawer v-model:charge-id="opened" />
    <ManualChargeDialog v-model:visible="manualDialog" :contract-id="contract.id" />
    <Dialog
      v-model:visible="utilDialog"
      modal
      header="Начисление ЖКУ за месяц"
      :style="{ width: '420px' }"
      :breakpoints="{ '460px': '100vw' }"
    >
      <form class="form" novalidate @submit.prevent="createUtilities">
        <div class="field">
          <label for="u-month">Месяц потребления *</label>
          <DatePicker
            v-model="month"
            input-id="u-month"
            view="month"
            date-format="mm.yy"
            show-icon
            icon-display="input"
            fluid
          />
        </div>
        <p class="hint">По счётчикам без принятого показания расход будет принят равным 0.</p>
        <Message v-if="utilError" severity="error" :closable="false">{{ utilError }}</Message>
        <div class="actions">
          <Button type="button" label="Отмена" outlined @click="utilDialog = false" />
          <Button type="submit" label="Сформировать" :loading="m.utilities.isPending.value" />
        </div>
      </form>
    </Dialog>
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
