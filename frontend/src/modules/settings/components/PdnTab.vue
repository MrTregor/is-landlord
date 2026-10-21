<script setup lang="ts">
// Настройки → ПДн (14 Экраны собственника, раздел 16.8): срок хранения, цель отдельного согласия,
// политика обработки ПДн и её версия. «Опубликовать новую версию» увеличивает версию — наниматели
// при следующем входе ознакомятся с ней заново.
import { computed, nextTick, ref, watch } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import RichTextEditor from '@/components/RichTextEditor.vue'
import { toInput, useOrganizationSettings, useSaveOrganizationSettings } from '@/api/queries/settings'
import { parseProblem } from '@/utils/problem'
import { useSaveState } from '@/utils/saveState'
import { nextPolicyVersion, pdnSchema } from '../schemas'

const settings = useOrganizationSettings()
const save = useSaveOrganizationSettings()
const toast = useToast()
const confirm = useConfirm()

const retention = ref<number | null>(3)
const purpose = ref('')
const html = ref('')
const errors = ref<Record<string, string>>({})
const serverError = ref<string | null>(null)
// Состояние кнопки «Сохранить» — только для срока хранения и цели согласия; публикация политики — своя кнопка.
const saveState = useSaveState(() => [retention.value, purpose.value])

watch(
  () => settings.data.value,
  (s) => {
    if (!s) return
    retention.value = s.pdnRetentionYears
    purpose.value = s.pdnConsentPurpose
    html.value = s.privacyPolicyHtml
  },
  { immediate: true },
)

const policyChanged = computed(() => !!settings.data.value && html.value !== settings.data.value.privacyPolicyHtml)
const version = computed(() => settings.data.value?.privacyPolicyVersion ?? '1.0')

async function persist(publish: boolean) {
  if (!settings.data.value) return
  serverError.value = null
  if (!publish) saveState.reset()
  const parsed = pdnSchema.safeParse({ pdnRetentionYears: retention.value, pdnConsentPurpose: purpose.value })
  errors.value = {}
  if (!parsed.success) {
    for (const i of parsed.error.issues) errors.value[String(i.path[0])] = i.message
    return
  }
  try {
    await save.mutateAsync({
      ...toInput(settings.data.value),
      ...parsed.data,
      privacyPolicyHtml: publish ? html.value : settings.data.value.privacyPolicyHtml,
      privacyPolicyVersion: publish ? nextPolicyVersion(version.value) : version.value,
    })
    await nextTick()
    if (!publish) saveState.saved()
    toast.add({
      severity: 'success',
      summary: publish ? 'Опубликована новая версия политики' : 'Сохранено',
      life: 3000,
    })
  } catch (err) {
    serverError.value = parseProblem(err).message
    if (!publish) saveState.failed()
  }
}

function publish() {
  confirm.require({
    header: 'Опубликовать новую версию политики?',
    message: `Версия ${version.value} → ${nextPolicyVersion(version.value)}. Наниматели при следующем входе ознакомятся с новой версией.`,
    acceptLabel: 'Опубликовать',
    rejectLabel: 'Отмена',
    accept: () => persist(true),
  })
}
</script>

<template>
  <Skeleton v-if="settings.isPending.value" height="20rem" />
  <div v-else class="form">
    <div class="form-grid">
      <div class="field">
        <label for="pdn-years">Срок хранения после окончания договора, лет *</label>
        <InputNumber
          v-model="retention"
          input-id="pdn-years"
          :min="1"
          :max="10"
          show-buttons
          :invalid="!!errors.pdnRetentionYears"
          fluid
        />
        <small v-if="errors.pdnRetentionYears" class="error">{{ errors.pdnRetentionYears }}</small>
        <small v-else class="hint"
          >3 года — срок исковой давности; до 5 лет — срок хранения документов налогового учёта</small
        >
      </div>
      <div class="field field--wide">
        <label for="pdn-purpose">Цель отдельного согласия *</label>
        <Textarea id="pdn-purpose" v-model="purpose" auto-resize rows="2" :invalid="!!errors.pdnConsentPurpose" fluid />
        <small v-if="errors.pdnConsentPurpose" class="error">{{ errors.pdnConsentPurpose }}</small>
        <small v-else class="hint">Для договора согласие не нужно; оно требуется только для этой цели</small>
      </div>
    </div>
    <div class="actions">
      <Button
        :label="saveState.button.value.label"
        :icon="saveState.button.value.icon"
        :severity="saveState.button.value.severity"
        :loading="save.isPending.value"
        @click="persist(false)"
      />
    </div>

    <h3 id="pdn-policy-title" class="subtitle">Политика обработки персональных данных · версия {{ version }}</h3>
    <p v-pre class="hint">
      Поля: {{ org.full_name }}, {{ org.inn }}, {{ org.address }}, {{ org.email }}, {{ pdn_retention_years }},
      {{ pdn_consent_purpose }}
    </p>
    <RichTextEditor v-model="html" labelled-by="pdn-policy-title" />
    <div class="actions">
      <a href="/privacy" target="_blank" rel="noopener">Открыть опубликованную политику</a>
      <Button
        label="Опубликовать новую версию"
        icon="pi pi-send"
        :disabled="!policyChanged"
        :loading="save.isPending.value"
        @click="publish"
      />
    </div>
    <Message v-if="serverError" severity="error" :closable="false">{{ serverError }}</Message>
  </div>
</template>

<style scoped>
.subtitle {
  margin: 1rem 0 0;
  font-size: 1.125rem;
}
.actions a {
  align-self: center;
  margin-right: auto;
}
</style>
