<script setup lang="ts">
// Настройки → Налоги и фискализация (14 Экраны собственника, раздел 16.2).
import { computed, nextTick, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { toInput, useOrganizationSettings, useSaveOrganizationSettings } from '@/api/queries/settings'
import type { components } from '@/api/schema'
import { fiscalModeLabels, taxRegimeLabels } from '@/utils/labels'
import { parseProblem } from '@/utils/problem'
import { useSaveState } from '@/utils/saveState'

type S = components['schemas']

const settings = useOrganizationSettings()
const save = useSaveOrganizationSettings()
const toast = useToast()

const taxRegime = ref<S['TaxRegime']>('usn_income')
const taxRate = ref<string>('6.00')
const vatPayer = ref(false)
const fiscalResidential = ref<S['FiscalMode']>('none')
const fiscalNonResidential = ref<S['FiscalMode']>('cloud_kkt')
const rateError = ref<string | null>(null)
const serverError = ref<string | null>(null)
const saveState = useSaveState(() => [
  taxRegime.value,
  taxRate.value,
  vatPayer.value,
  fiscalResidential.value,
  fiscalNonResidential.value,
])

watch(
  () => settings.data.value,
  (s) => {
    if (!s) return
    taxRegime.value = s.taxRegime
    taxRate.value = s.taxRatePct
    vatPayer.value = s.vatPayer
    fiscalResidential.value = s.defaultFiscalModeResidential
    fiscalNonResidential.value = s.defaultFiscalModeNonResidential
  },
  { immediate: true },
)

const regimes = Object.entries(taxRegimeLabels).map(([value, label]) => ({ value, label }))
const fiscalModes = Object.entries(fiscalModeLabels).map(([value, label]) => ({ value, label }))
const isUsn = computed(() => taxRegime.value === 'usn_income' || taxRegime.value === 'usn_income_expense')

// Ставка в процентах: строка с запятой или точкой, до 2 знаков, 0–30.
function normalizeRate(v: string): string | null {
  const s = v.trim().replace(',', '.')
  if (!/^\d{1,2}(\.\d{1,2})?$/.test(s)) return null
  const [int = '0', frac = ''] = s.split('.')
  if (Number(int) > 30 || (Number(int) === 30 && /[1-9]/.test(frac))) return null
  return `${int}.${frac.padEnd(2, '0')}`
}

async function submit() {
  if (!settings.data.value) return
  rateError.value = null
  serverError.value = null
  saveState.reset()
  const rate = normalizeRate(taxRate.value)
  if (rate === null) {
    rateError.value = 'Ставка — число от 0 до 30, не более двух знаков после запятой'
    return
  }
  try {
    await save.mutateAsync({
      ...toInput(settings.data.value),
      taxRegime: taxRegime.value,
      taxRatePct: rate,
      vatPayer: vatPayer.value,
      defaultFiscalModeResidential: fiscalResidential.value,
      defaultFiscalModeNonResidential: fiscalNonResidential.value,
    })
    await nextTick()
    saveState.saved()
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
  } catch (err) {
    serverError.value = parseProblem(err).message
    saveState.failed()
  }
}
</script>

<template>
  <Skeleton v-if="settings.isPending.value" height="14rem" />
  <form v-else class="form" novalidate @submit.prevent="submit">
    <div class="form-grid">
      <div class="field">
        <label for="tax-regime">Режим налогообложения *</label>
        <Select
          v-model="taxRegime"
          input-id="tax-regime"
          :options="regimes"
          option-label="label"
          option-value="value"
          fluid
        />
      </div>
      <div v-if="isUsn" class="field">
        <label for="tax-rate">Ставка, % *</label>
        <InputText id="tax-rate" v-model="taxRate" inputmode="decimal" :invalid="!!rateError" fluid />
        <small v-if="rateError" class="error">{{ rateError }}</small>
      </div>
      <div class="field">
        <label for="fiscal-res">Режим чека по умолчанию для жилых объектов *</label>
        <Select
          v-model="fiscalResidential"
          input-id="fiscal-res"
          :options="fiscalModes"
          option-label="label"
          option-value="value"
          fluid
        />
      </div>
      <div class="field">
        <label for="fiscal-nonres">Режим чека по умолчанию для нежилых объектов *</label>
        <Select
          v-model="fiscalNonResidential"
          input-id="fiscal-nonres"
          :options="fiscalModes"
          option-label="label"
          option-value="value"
          fluid
        />
        <small v-if="fiscalNonResidential === 'none'" class="hint">
          По нежилым объектам освобождение от применения ККТ не действует
        </small>
      </div>
      <div class="field field--check field--wide">
        <ToggleSwitch v-model="vatPayer" input-id="vat-payer" />
        <label for="vat-payer">Плательщик НДС</label>
        <small class="hint">Чеки с НДС не поддерживаются</small>
      </div>
    </div>
    <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
    <div class="actions">
      <Button
        type="submit"
        :label="saveState.button.value.label"
        :icon="saveState.button.value.icon"
        :severity="saveState.button.value.severity"
        :loading="save.isPending.value"
      />
    </div>
  </form>
</template>
