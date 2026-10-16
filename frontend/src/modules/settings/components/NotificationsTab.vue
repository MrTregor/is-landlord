<script setup lang="ts">
// Настройки → Уведомления и сроки (14 Экраны собственника, раздел 16, вкладка 3; FR-10.3): напоминание об оплате,
// повтор письма о просрочке, дни уведомления об окончании договора, день напоминания о показаниях, автоматическое
// принятие показаний, утренняя сводка, срок подготовки индексации.
import { reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { toInput, useOrganizationSettings, useSaveOrganizationSettings } from '@/api/queries/settings'
import { parseProblem } from '@/utils/problem'
import { parseNoticeDays } from '../notice'

const settings = useOrganizationSettings()
const save = useSaveOrganizationSettings()
const toast = useToast()

const form = reactive({
  remindBeforeDueDays: 3,
  overdueRepeatDays: 7,
  noticeDays: '90, 30',
  readingsReminderDay: 20,
  readingsAutoAccept: false,
  ownerDailyDigest: true,
  indexationDraftDaysBefore: 30,
})
const errors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)

watch(
  () => settings.data.value,
  (s) => {
    if (!s) return
    Object.assign(form, {
      remindBeforeDueDays: s.remindBeforeDueDays,
      overdueRepeatDays: s.overdueRepeatDays,
      noticeDays: s.contractEndNoticeDays.join(', '),
      readingsReminderDay: s.readingsReminderDay,
      readingsAutoAccept: s.readingsAutoAccept,
      ownerDailyDigest: s.ownerDailyDigest,
      indexationDraftDaysBefore: s.indexationDraftDaysBefore,
    })
  },
  { immediate: true },
)

async function submit() {
  if (!settings.data.value) return
  errors.value = {}
  serverError.value = null
  const days = parseNoticeDays(form.noticeDays)
  if (typeof days === 'string') {
    errors.value = { contractEndNoticeDays: days }
    return
  }
  try {
    await save.mutateAsync({
      ...toInput(settings.data.value),
      remindBeforeDueDays: form.remindBeforeDueDays,
      overdueRepeatDays: form.overdueRepeatDays,
      contractEndNoticeDays: days,
      readingsReminderDay: form.readingsReminderDay,
      readingsAutoAccept: form.readingsAutoAccept,
      ownerDailyDigest: form.ownerDailyDigest,
      indexationDraftDaysBefore: form.indexationDraftDaysBefore,
    })
    toast.add({ severity: 'success', summary: 'Сохранено', life: 3000 })
  } catch (err) {
    const p = parseProblem(err)
    errors.value = p.fieldErrors
    serverError.value = Object.keys(p.fieldErrors).length ? null : p.message
  }
}
</script>

<template>
  <Skeleton v-if="settings.isPending.value" height="16rem" />
  <form v-else class="form" novalidate @submit.prevent="submit">
    <div class="form-grid">
      <div class="field">
        <label for="n-remind">За сколько дней напоминать об оплате *</label>
        <InputNumber v-model="form.remindBeforeDueDays" input-id="n-remind" :min="0" :max="15" show-buttons fluid />
        <small class="hint">0–15 дней до срока оплаты</small>
        <small v-if="errors.remindBeforeDueDays" class="error">{{ errors.remindBeforeDueDays }}</small>
      </div>
      <div class="field">
        <label for="n-repeat">Повтор письма о просрочке, дней *</label>
        <InputNumber v-model="form.overdueRepeatDays" input-id="n-repeat" :min="1" :max="30" show-buttons fluid />
        <small v-if="errors.overdueRepeatDays" class="error">{{ errors.overdueRepeatDays }}</small>
      </div>
      <div class="field">
        <label for="n-end">Уведомлять об окончании договора за, дней *</label>
        <InputText
          id="n-end"
          v-model="form.noticeDays"
          :invalid="!!errors.contractEndNoticeDays"
          placeholder="90, 30"
          fluid
        />
        <small class="hint">Числа через запятую, например 90, 30</small>
        <small v-if="errors.contractEndNoticeDays" class="error">{{ errors.contractEndNoticeDays }}</small>
      </div>
      <div class="field">
        <label for="n-readings">День напоминания о показаниях *</label>
        <InputNumber v-model="form.readingsReminderDay" input-id="n-readings" :min="1" :max="28" show-buttons fluid />
        <small class="hint">С этого числа по 5-е следующего месяца наниматель передаёт показания</small>
        <small v-if="errors.readingsReminderDay" class="error">{{ errors.readingsReminderDay }}</small>
      </div>
      <div class="field">
        <label for="n-index">За сколько дней готовить индексацию *</label>
        <InputNumber
          v-model="form.indexationDraftDaysBefore"
          input-id="n-index"
          :min="7"
          :max="90"
          show-buttons
          fluid
        />
        <small v-if="errors.indexationDraftDaysBefore" class="error">{{ errors.indexationDraftDaysBefore }}</small>
      </div>
      <div class="field field--check field--wide">
        <ToggleSwitch v-model="form.readingsAutoAccept" input-id="n-auto" />
        <label for="n-auto">Автоматически принимать показания нанимателя</label>
      </div>
      <div class="field field--check field--wide">
        <ToggleSwitch v-model="form.ownerDailyDigest" input-id="n-digest" />
        <label for="n-digest">Утренняя сводка собственнику (08:00)</label>
      </div>
    </div>
    <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
    <div class="actions">
      <Button type="submit" label="Сохранить" icon="pi pi-check" :loading="save.isPending.value" />
    </div>
  </form>
</template>
