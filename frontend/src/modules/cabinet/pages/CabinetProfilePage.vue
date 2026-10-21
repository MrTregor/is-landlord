<script setup lang="ts">
// Профиль нанимателя (15 Экраны нанимателя, раздел 8): имя и e-mail (только чтение), телефон, смена пароля,
// политика и дата ознакомления, отдельное согласие на обработку ПДн (дать / отозвать), контакты организации, выход.
import { nextTick, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { useCabinetProfile, useCabinetProfileMutations } from '@/api/queries/cabinet'
import ChangePasswordDialog from '@/components/ChangePasswordDialog.vue'
import { useSessionStore } from '@/app/session'
import { formatDate, formatDateTime } from '@/utils/date'
import { formatPhone, normalizePhone } from '@/utils/phone'
import { parseProblem } from '@/utils/problem'
import { useSaveState } from '@/utils/saveState'

const router = useRouter()
const toast = useToast()
const session = useSessionStore()
const profile = useCabinetProfile()
const m = useCabinetProfileMutations()
const p = profile.data

const phone = ref('')
const phoneError = ref<string | null>(null)
// Номер после сохранения возвращается отформатированным — сравнивается нормализованное значение.
const phoneState = useSaveState(() => normalizePhone(phone.value.trim()) ?? phone.value.trim())
watch(
  () => p.value?.phone,
  (v) => {
    phone.value = formatPhone(v)
  },
  { immediate: true },
)

async function savePhone() {
  phoneError.value = null
  phoneState.reset()
  const raw = phone.value.trim()
  const next = raw ? normalizePhone(raw) : null
  if (raw && !next) {
    phoneError.value = 'Телефон — 10 цифр после +7'
    return
  }
  try {
    await m.phone.mutateAsync(next)
    await nextTick()
    phoneState.saved()
    toast.add({ severity: 'success', summary: 'Телефон сохранён', life: 3000 })
  } catch (e) {
    const pr = parseProblem(e)
    phoneError.value = pr.fieldErrors.phone ?? pr.message
    phoneState.failed()
  }
}

const passwordDialog = ref(false)

const agree = ref(false)
async function giveConsent() {
  try {
    await m.consent.mutateAsync()
    agree.value = false
    toast.add({ severity: 'success', summary: 'Согласие сохранено', life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(e).message, life: 6000 })
  }
}
async function revokeConsent() {
  try {
    await m.revoke.mutateAsync()
    toast.add({ severity: 'success', summary: 'Согласие отозвано', life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Ошибка', detail: parseProblem(e).message, life: 6000 })
  }
}

async function copy(text: string | null | undefined) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    toast.add({ severity: 'success', summary: 'Скопировано', life: 2000 })
  } catch {
    toast.add({ severity: 'warn', summary: 'Не удалось скопировать', life: 3000 })
  }
}

async function logout() {
  await session.logout().catch(() => undefined)
  await router.push('/login')
}
</script>

<template>
  <div class="profile">
    <h1 class="title">Профиль</h1>
    <Skeleton v-if="profile.isPending.value" height="12rem" />
    <Message v-else-if="profile.isError.value" severity="error" :closable="false">
      {{ parseProblem(profile.error.value).message }}
    </Message>
    <template v-else-if="p">
      <section class="card">
        <div class="field">
          <span class="label">Имя</span>
          <span>{{ p.displayName }}</span>
        </div>
        <div class="field">
          <span class="label">E-mail</span>
          <span class="wrap">{{ p.email }}</span>
          <small class="hint">Для смены e-mail обратитесь к {{ p.organization.shortName }}</small>
        </div>
        <form class="field" novalidate @submit.prevent="savePhone">
          <label for="pf-phone">Телефон</label>
          <div class="inline">
            <InputText
              id="pf-phone"
              v-model="phone"
              type="tel"
              inputmode="tel"
              autocomplete="tel"
              placeholder="+7 (900) 000-00-00"
              :invalid="!!phoneError"
              fluid
            />
            <Button
              type="submit"
              :label="phoneState.button.value.label"
              :icon="phoneState.button.value.icon"
              :severity="phoneState.button.value.severity"
              :outlined="phoneState.state.value === 'idle'"
              :loading="m.phone.isPending.value"
              class="touch"
            />
          </div>
          <small v-if="phoneError" class="error">{{ phoneError }}</small>
        </form>
        <Button label="Сменить пароль" icon="pi pi-key" outlined class="wide" @click="passwordDialog = true" />
      </section>

      <section class="card">
        <h2>Персональные данные</h2>
        <p>
          <RouterLink to="/privacy" target="_blank">Политика обработки персональных данных</RouterLink>
          <span v-if="p.policyAcceptedAt" class="hint">
            — ознакомлены {{ formatDateTime(p.policyAcceptedAt) }} (версия {{ p.policyAcceptedVersion }})
          </span>
        </p>
        <div v-if="p.consentAvailable" class="consent">
          <h3>Согласие на обработку ПДн</h3>
          <p class="purpose">{{ p.consentPurpose }}</p>
          <template v-if="p.consent">
            <p>Согласие дано {{ formatDate(p.consent.givenOn) }}</p>
            <Button
              label="Отозвать согласие"
              severity="danger"
              outlined
              class="wide"
              :loading="m.revoke.isPending.value"
              @click="revokeConsent"
            />
          </template>
          <template v-else>
            <div class="check">
              <Checkbox v-model="agree" input-id="pf-consent" binary />
              <label for="pf-consent">Даю согласие</label>
            </div>
            <Button
              label="Сохранить"
              class="wide"
              :disabled="!agree"
              :loading="m.consent.isPending.value"
              @click="giveConsent"
            />
          </template>
          <small class="hint">Необязательно, на договор и оплату не влияет</small>
        </div>
      </section>

      <section class="card">
        <h2>Контакты {{ p.organization.shortName }}</h2>
        <div v-if="p.organization.phone" class="contact">
          <a :href="`tel:${p.organization.phone}`">{{ formatPhone(p.organization.phone) }}</a>
          <Button
            icon="pi pi-copy"
            text
            aria-label="Скопировать телефон"
            class="touch"
            @click="copy(p.organization.phone)"
          />
        </div>
        <div class="contact">
          <a :href="`mailto:${p.organization.email}`" class="wrap">{{ p.organization.email }}</a>
          <Button
            icon="pi pi-copy"
            text
            aria-label="Скопировать e-mail"
            class="touch"
            @click="copy(p.organization.email)"
          />
        </div>
      </section>

      <Button label="Выйти" icon="pi pi-sign-out" severity="secondary" outlined class="wide" @click="logout" />
      <ChangePasswordDialog v-model:visible="passwordDialog" />
    </template>
  </div>
</template>

<style scoped>
.profile {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.title {
  font-size: 1.25rem;
  margin: 0;
}
.card {
  background: var(--p-content-background);
  border: 1px solid var(--p-content-border-color);
  border-radius: 8px;
  padding: 0.75rem 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  min-width: 0;
}
.card h2 {
  margin: 0;
  font-size: 1rem;
  color: var(--p-text-muted-color);
}
.card h3 {
  margin: 0;
  font-size: 1rem;
}
.card p {
  margin: 0;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.label,
.field > label {
  color: var(--p-text-muted-color);
  font-size: 0.875rem;
}
.inline {
  display: flex;
  gap: 0.5rem;
}
.inline :deep(input) {
  min-height: 44px;
  min-width: 0;
}
.wrap {
  overflow-wrap: anywhere;
}
.consent {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding-top: 0.5rem;
  border-top: 1px solid var(--p-content-border-color);
}
.purpose {
  white-space: pre-wrap;
}
.check {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-height: 44px;
}
.contact {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  min-width: 0;
}
.error {
  color: var(--p-red-600);
}
.touch {
  min-width: 44px;
  min-height: 44px;
  flex-shrink: 0;
}
.wide {
  width: 100%;
  min-height: 44px;
}
</style>
