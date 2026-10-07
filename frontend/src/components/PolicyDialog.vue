<script setup lang="ts">
// Окно ознакомления с политикой ПДн (15 Экраны нанимателя, раздел 1): закрыть можно только «Продолжить» или «Выйти».
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { api, unwrap } from '@/api/client'
import { useSessionStore } from '@/app/session'
import { parseProblem } from '@/utils/problem'

const session = useSessionStore()
const router = useRouter()
const qc = useQueryClient()
const policy = useQuery({
  queryKey: ['public', 'privacy-policy'],
  queryFn: () => unwrap(api.GET('/public/privacy-policy')),
})
const agree = ref(false)
const busy = ref(false)
const error = ref<string | null>(null)

async function accept() {
  if (!policy.data.value) return
  busy.value = true
  error.value = null
  try {
    await unwrap(api.POST('/auth/accept-policy', { body: { version: policy.data.value.version } }))
    session.mustAcceptPolicy = false
    await qc.invalidateQueries()
  } catch (err) {
    error.value = parseProblem(err).message
    await qc.invalidateQueries({ queryKey: ['public', 'privacy-policy'] })
  } finally {
    busy.value = false
  }
}

async function logout() {
  await session.logout().catch(() => undefined)
  await router.push('/login')
}
</script>

<template>
  <Dialog
    :visible="true"
    modal
    :closable="false"
    :close-on-escape="false"
    header="Политика обработки персональных данных"
    :style="{ width: '720px' }"
    :breakpoints="{ '767px': '100vw' }"
  >
    <Skeleton v-if="policy.isPending.value" height="12rem" />
    <!-- eslint-disable-next-line vue/no-v-html -- текст политики очищается сервером (bluemonday) -->
    <div v-else class="policy-text" v-html="policy.data.value?.html" />
    <div class="policy-agree">
      <Checkbox v-model="agree" input-id="policy-agree" binary />
      <label for="policy-agree">Я ознакомлен(а) с Политикой обработки персональных данных</label>
    </div>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <template #footer>
      <Button label="Выйти" outlined @click="logout" />
      <Button label="Продолжить" :disabled="!agree" :loading="busy" @click="accept" />
    </template>
  </Dialog>
</template>

<style scoped>
.policy-text {
  max-height: 50vh;
  overflow-y: auto;
  padding-right: 0.5rem;
}
.policy-agree {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-height: 44px;
  margin-top: 1rem;
}
</style>
